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
    assert.equal(ui.fallbackHintForRole("invoice", { country: "PL" }), "Empty — invoice consumers will use the general address.");
    assert.equal(ui.fallbackHintForRole("returns", { country: "PL" }), "Empty — returns will use shipping origin, then general.");
    assert.equal(ui.fallbackHintForRole("invoice", { line1: "A", country: "PL" }), "");
    assert.equal(ui.fallbackHintForRole("general", { country: "PL" }), "");
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

test("buildAddressPutBody sends all address fields", function () {
    const body = ui.buildAddressPutBody({
        line1: "A",
        line2: "",
        city: "B",
        region: "",
        postcode: "00-001",
        country: "PL",
        phone: ""
    });
    assert.deepEqual(Object.keys(body).sort(), [
        "city", "country", "line1", "line2", "phone", "postcode", "region"
    ]);
});

test("load gate drops stale generation and mismatched store", function () {
    const gate = ui.newStoreAddressesLoadGate();
    const first = gate.start("store-a");
    assert.equal(first.isCurrent("store-a"), true);
    const second = gate.start("store-b");
    assert.equal(first.isCurrent("store-a"), false);
    assert.equal(second.isCurrent("store-b"), true);
    assert.equal(ui.shouldApplyWriteResult(first.storeID, "store-a"), true);
    assert.equal(ui.shouldApplyWriteResult(first.storeID, "store-b"), false);
});

test("headerContextChangeRequiresRemount skips Store Addresses paths", function () {
    assert.equal(ui.headerContextChangeRequiresRemount("/admin/settings/store-addresses"), false);
    assert.equal(ui.headerContextChangeRequiresRemount("/admin/settings/store-addresses/invoice"), false);
    assert.equal(ui.headerContextChangeRequiresRemount("/admin/settings"), true);
});

test("formatAddressSummary and indexByRole", function () {
    assert.equal(ui.formatAddressSummary({ country: "PL" }), "Not set");
    assert.match(ui.formatAddressSummary({ line1: "Main", postcode: "00-001", city: "Warsaw", country: "PL" }), /Main/);
    const map = ui.indexByRole([
        { role: "general", line1: "A" },
        { role: "invoice", line1: "B" }
    ]);
    assert.equal(map.general.line1, "A");
    assert.equal(map.invoice.line1, "B");
});

test("admin.js wires Store Addresses routes and nav", function () {
    const adminSrc = fs.readFileSync(path.join(__dirname, "dist", "admin.js"), "utf8");
    const indexSrc = fs.readFileSync(path.join(__dirname, "dist", "index.html"), "utf8");

    assert.match(indexSrc, /href="\/admin\/settings\/store-addresses"/);
    assert.match(indexSrc, /store_addresses_admin_ui\.js/);
    assert.match(adminSrc, /\/admin\/settings\/store-addresses/);
    assert.match(adminSrc, /renderStoreAddressesIndexPage/);
    assert.match(adminSrc, /renderStoreAddressRolePage/);
    assert.match(adminSrc, /newStoreAddressesLoadGate/);
    assert.match(adminSrc, /applyAddressValues/);
    assert.ok(adminSrc.indexOf("/admin/store-addresses/") >= 0);
    assert.ok(adminSrc.indexOf("encodeURIComponent(normalizedRole)") >= 0 ||
        adminSrc.indexOf("encodeURIComponent(role)") >= 0);
});
