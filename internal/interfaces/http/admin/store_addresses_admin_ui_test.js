"use strict";

const { test } = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const ui = require("./dist/store_addresses_admin_ui.js");

test("addressRoles lists Magento-like roles in order", function () {
    assert.deepEqual(ui.addressRoles().map(function (r) { return r.role; }), [
        "general", "invoice", "shipping_origin", "returns"
    ]);
    assert.equal(ui.isValidAddressRole("invoice"), true);
    assert.equal(ui.isValidAddressRole("warehouse"), false);
});

test("addressIsEmpty mirrors postal-content emptiness", function () {
    assert.equal(ui.addressIsEmpty({ country: "PL", phone: "+48" }), true);
    assert.equal(ui.addressIsEmpty({ line1: "Main", country: "PL" }), false);
    assert.equal(ui.addressIsEmpty({ line2: "Apt 2", country: "PL" }), false);
    assert.equal(ui.fallbackHintForRole("invoice", { country: "PL" }), "Empty — invoice consumers will use the general address.");
    assert.equal(ui.fallbackHintForRole("INVOICE", { country: "PL" }), "Empty — invoice consumers will use the general address.");
    assert.match(ui.fallbackHintForRole("returns", { country: "PL", phone: "+481" }), /Phone is stored/);
    assert.equal(ui.fallbackHintForRole("invoice", { line1: "A", country: "PL" }), "");
    assert.equal(ui.fallbackHintForRole("general", { country: "PL" }), "");
});

test("formatAddressSummary includes line2 and region", function () {
    assert.equal(ui.formatAddressSummary({ country: "PL" }), "Not set");
    const line2Only = ui.formatAddressSummary({ line2: "Suite B", country: "DE" });
    assert.match(line2Only, /Suite B/);
    assert.match(line2Only, /DE/);
    assert.ok(line2Only.indexOf("Not set") < 0);
    const regionOnly = ui.formatAddressSummary({ region: "MZ", country: "PL" });
    assert.match(regionOnly, /MZ/);
});

test("applyAddressValues sets DOM properties including quotes", function () {
    const fields = {};
    ["line1", "line2", "city", "region", "postcode", "country", "phone"].forEach(function (name) {
        fields[name] = { value: "" };
    });
    ui.applyAddressValues(fields, {
        line1: 'Ul. "Test" 1',
        city: "Warsaw",
        country: "PL",
        phone: '1"2'
    });
    assert.equal(fields.line1.value, 'Ul. "Test" 1');
    assert.equal(fields.phone.value, '1"2');
    assert.equal(fields.country.value, "PL");
});

test("buildAddressPutBody keeps empty-string clears", function () {
    const body = ui.buildAddressPutBody({
        line1: "",
        line2: "",
        city: "",
        region: "",
        postcode: "",
        country: "PL",
        phone: ""
    });
    assert.deepEqual(body, {
        line1: "",
        line2: "",
        city: "",
        region: "",
        postcode: "",
        country: "PL",
        phone: ""
    });
    assert.equal(Object.keys(body).length, 7);
});

test("fieldMaxLengths match domain caps", function () {
    assert.deepEqual(ui.fieldMaxLengths(), {
        line1: 255,
        line2: 255,
        city: 255,
        region: 255,
        postcode: 64,
        country: 2,
        phone: 64
    });
});

test("load gate drops stale generation and mismatched store", function () {
    const gate = ui.newStoreAddressesLoadGate();
    const first = gate.start("store-a");
    assert.equal(first.isCurrent("store-a"), true);
    const second = gate.start("store-b");
    assert.equal(first.isCurrent("store-a"), false);
    assert.equal(second.isCurrent("store-b"), true);
});

test("stale generation does not mutate another role page", function () {
    const gate = ui.newStoreAddressesLoadGate();
    const invoiceTicket = gate.start("store-a");
    // Navigate to Returns (same store) — bumps generation.
    const returnsTicket = gate.start("store-a");
    assert.equal(invoiceTicket.isCurrent("store-a"), false);
    assert.equal(returnsTicket.isCurrent("store-a"), true);

    const messages = [];
    function setSettingsMessage(id, text) {
        messages.push({ id: id, text: text });
    }

    // Invoice PUT completes while Returns is on screen.
    const liveFormRole = "returns";
    const writeRole = "invoice";
    if (ui.shouldMutateAddressRolePage(invoiceTicket, "store-a", liveFormRole, writeRole)) {
        setSettingsMessage("store-address-role-msg", "Address saved.");
    }
    if (ui.shouldRefetchAddressRoleAfterWrite(invoiceTicket.storeID, "store-a", liveFormRole, writeRole)) {
        setSettingsMessage("store-address-role-msg", "would-refetch");
    }
    assert.deepEqual(messages, [], "cross-role completion must not touch the live page");

    // Same-role remount: store matches and live form is still invoice → refetch allowed.
    assert.equal(
        ui.shouldRefetchAddressRoleAfterWrite(invoiceTicket.storeID, "store-a", "invoice", "invoice"),
        true
    );
    assert.equal(
        ui.shouldMutateAddressRolePage(invoiceTicket, "store-a", "invoice", "invoice"),
        false,
        "stale generation must not treat mutate as current"
    );
});

test("formatAddressSummary and indexByRole", function () {
    assert.match(ui.formatAddressSummary({ line1: "Main", postcode: "00-001", city: "Warsaw", country: "PL" }), /Main/);
    const map = ui.indexByRole([
        { role: "general", line1: "A" },
        { role: "invoice", line1: "B" }
    ]);
    assert.equal(map.general.line1, "A");
    assert.equal(map.invoice.line1, "B");
});

test("admin.js wires Store Addresses routes, gate checks, and autocomplete", function () {
    const adminSrc = fs.readFileSync(path.join(__dirname, "dist", "admin.js"), "utf8");
    const indexSrc = fs.readFileSync(path.join(__dirname, "dist", "index.html"), "utf8");

    assert.match(indexSrc, /href="\/admin\/settings\/store-addresses"/);
    assert.match(indexSrc, /store_addresses_admin_ui\.js/);
    assert.match(adminSrc, /renderStoreAddressesIndexPage/);
    assert.match(adminSrc, /renderStoreAddressRolePage/);
    assert.match(adminSrc, /newStoreAddressesLoadGate/);
    assert.match(adminSrc, /shouldMutateAddressRolePage/);
    assert.match(adminSrc, /shouldRefetchAddressRoleAfterWrite/);
    assert.match(adminSrc, /refreshStoreAddressRoleForm/);
    assert.match(adminSrc, /data-role/);
    assert.match(adminSrc, /section-store address-line1/);
    assert.match(adminSrc, /reportValidity/);
    assert.ok(adminSrc.indexOf('maxlength="255"') >= 0 || adminSrc.indexOf("max.city") >= 0);
    assert.ok(adminSrc.indexOf("/admin/store-addresses/") >= 0);
    // Single remount predicate — addresses helper must not short-circuit admin.js alone.
    assert.equal(adminSrc.indexOf("ShopandaStoreAddressesAdminUI.headerContextChangeRequiresRemount"), -1);
});
