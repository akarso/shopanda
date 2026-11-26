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

test("formatAddressSummary includes line2, region, and phone-only", function () {
    assert.equal(ui.formatAddressSummary({ country: "PL" }), "Not set");
    assert.equal(ui.formatAddressSummary({ country: "PL", phone: "+48123" }), "Phone only: +48123");
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
    gate.start("store-a"); // Returns remount

    assert.equal(
        ui.shouldRefetchAddressRoleAfterWrite(invoiceTicket.storeID, "store-a", "returns", "invoice"),
        false
    );
    assert.equal(
        ui.shouldShowAddressWriteFeedback(invoiceTicket.storeID, "store-a", "invoice", "invoice"),
        true,
        "same-role remount may still show PUT error/success"
    );
    assert.equal(
        ui.shouldMutateAddressRolePage(invoiceTicket, "store-a", "invoice", "invoice"),
        false
    );
});

test("refetch fill applies only while captured store id is current", function () {
    const fillGate = ui.newAddressFillGate();
    const fills = [];
    function applyFill(ticket, currentStoreID, response, snapshot) {
        if (!ui.shouldApplyAddressFill(ticket, currentStoreID, response && response.store_id)) {
            return;
        }
        fills.push(snapshot);
    }

    const refetch = fillGate.begin("store-a");
    applyFill(refetch, "store-b", { store_id: "store-a" }, "paint-a-into-b");
    assert.deepEqual(fills, [], "store switch must drop the fill");

    const ok = fillGate.begin("store-a");
    applyFill(ok, "store-a", { store_id: "store-a" }, "paint-a");
    assert.deepEqual(fills, ["paint-a"]);

    applyFill(ok, "store-a", { store_id: "store-b" }, "wrong-response-store");
    assert.deepEqual(fills, ["paint-a"], "response store_id mismatch must drop");
});

test("later page GET does not overwrite an earlier refetch fill", function () {
    const fillGate = ui.newAddressFillGate();
    const paints = [];
    function paint(ticket, currentStoreID, snapshot) {
        if (!ui.shouldApplyAddressFill(ticket, currentStoreID, "store-a")) {
            return;
        }
        paints.push(snapshot);
    }

    // Remount page GET starts first (pre-write).
    const pageGet = fillGate.begin("store-a");
    // Post-write refetch starts second and must win.
    const refetch = fillGate.begin("store-a");
    paint(refetch, "store-a", "post-write");
    paint(pageGet, "store-a", "pre-write-late");
    assert.deepEqual(paints, ["post-write"]);
});

test("index refresh predicate requires connected grid and matching store", function () {
    assert.equal(ui.shouldRefreshAddressIndexAfterWrite("a", "a", true), true);
    assert.equal(ui.shouldRefreshAddressIndexAfterWrite("a", "b", true), false);
    assert.equal(ui.shouldRefreshAddressIndexAfterWrite("a", "a", false), false);
});

test("admin.js wires Store Addresses fill gate, novalidate, and feedback paths", function () {
    const adminSrc = fs.readFileSync(path.join(__dirname, "dist", "admin.js"), "utf8");
    const indexSrc = fs.readFileSync(path.join(__dirname, "dist", "index.html"), "utf8");

    assert.match(indexSrc, /href="\/admin\/settings\/store-addresses"/);
    assert.match(indexSrc, /store_addresses_admin_ui\.js/);
    assert.match(adminSrc, /newAddressFillGate/);
    assert.match(adminSrc, /shouldApplyAddressFill/);
    assert.match(adminSrc, /shouldShowAddressWriteFeedback/);
    assert.match(adminSrc, /refreshStoreAddressesIndex/);
    assert.match(adminSrc, /novalidate/);
    assert.match(adminSrc, /firstValidationMessage/);
    assert.match(adminSrc, /section-store address-line1/);
    assert.ok(adminSrc.indexOf("/admin/store-addresses/") >= 0);
    assert.equal(adminSrc.indexOf("ShopandaStoreAddressesAdminUI.headerContextChangeRequiresRemount"), -1);
});
