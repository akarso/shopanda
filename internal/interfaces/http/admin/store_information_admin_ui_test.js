"use strict";

const { test } = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const ui = require("./dist/store_information_admin_ui.js");

test("escAttr encodes quotes for attribute values", function () {
    const raw = 'Acme " autofocus onfocus="alert(1)';
    const encoded = ui.escAttr(raw);
    assert.ok(encoded.indexOf('"') < 0, "raw quote must not survive");
    assert.ok(encoded.indexOf("&quot;") >= 0, "quote must become &quot;");
    const html = '<input value="' + encoded + '">';
    assert.match(html, /value="Acme &quot; autofocus onfocus=&quot;alert\(1\)"/);
});

test("applyIdentityValues sets DOM properties (not attributes)", function () {
    const fields = {};
    ["legal_name", "phone", "hours", "email_general", "email_sales", "email_support"].forEach(function (name) {
        fields[name] = { value: "" };
    });
    ui.applyIdentityValues(fields, {
        legal_name: 'Corp " HQ',
        phone: '1"2',
        hours: "9-5",
        email_general: "a@b.c",
        email_sales: "",
        email_support: "s@b.c"
    });
    assert.equal(fields.legal_name.value, 'Corp " HQ');
    assert.equal(fields.phone.value, '1"2');
    assert.equal(fields.hours.value, "9-5");
    assert.equal(fields.email_general.value, "a@b.c");
    assert.equal(fields.email_support.value, "s@b.c");
});

test("applyGeneralStoreValues sets DOM properties including quotes", function () {
    const fields = {
        code: { value: "" },
        name: { value: "" },
        domain: { value: "" },
        country: { value: "" },
        language: { value: "" },
        currency: { value: "" },
        is_default: { checked: false }
    };
    ui.applyGeneralStoreValues(fields, {
        code: 'pl"x',
        name: 'Store " One',
        domain: "ex.com",
        country: "PL",
        language: "pl",
        currency: "PLN",
        is_default: true
    });
    assert.equal(fields.code.value, 'pl"x');
    assert.equal(fields.name.value, 'Store " One');
    assert.equal(fields.is_default.checked, true);
});

test("load gate drops stale generation and mismatched store", function () {
    const gate = ui.newStoreInfoLoadGate();
    const first = gate.start("store-a");
    assert.equal(first.isCurrent("store-a"), true);

    const second = gate.start("store-b");
    assert.equal(first.isCurrent("store-a"), false, "older generation must be stale");
    assert.equal(second.isCurrent("store-b"), true);
    assert.equal(second.isCurrent("store-a"), false, "store id mismatch must drop");

    const third = gate.start("store-b");
    assert.equal(second.isCurrent("store-b"), false);
    assert.equal(third.isCurrent("store-b"), true);
});

test("write result applies on same store even when page generation bumped", function () {
    const gate = ui.newStoreInfoLoadGate();
    const first = gate.start("store-a");
    gate.start("store-a"); // same-store remount (language/currency)
    assert.equal(first.isCurrent("store-a"), false);
    assert.equal(ui.shouldApplyWriteResult(first.storeID, "store-a"), true);
    assert.equal(ui.shouldApplyWriteResult(first.storeID, "store-b"), false);
});

test("older tax panel load does not paint after newer start", function () {
    const gate = ui.newTaxPanelLoadGate();
    const paints = [];
    function paint(ticket, snapshot) {
        if (!ticket.isCurrent()) {
            return;
        }
        paints.push(snapshot);
    }
    const older = gate.start();
    const newer = gate.start();
    paint(older, { taxIDs: ["old-vat"] });
    paint(newer, { taxIDs: ["new-vat"] });
    assert.deepEqual(paints, [{ taxIDs: ["new-vat"] }]);

    // A third load must also drop the previous newer ticket.
    const newest = gate.start();
    paint(newer, { taxIDs: ["stale-again"] });
    paint(newest, { taxIDs: ["latest"] });
    assert.deepEqual(paints, [{ taxIDs: ["new-vat"] }, { taxIDs: ["latest"] }]);
});

test("sidebarLinkIsActive exact-matches /admin/settings", function () {
    assert.equal(ui.sidebarLinkIsActive("/admin/settings", "/admin/settings"), true);
    assert.equal(ui.sidebarLinkIsActive("/admin/settings", "/admin/settings/store-information"), false);
    assert.equal(ui.sidebarLinkIsActive("/admin/settings/store-information", "/admin/settings/store-information"), true);
    assert.equal(ui.sidebarLinkIsActive("/admin/settings/store-information", "/admin/settings/store-information/extra"), true);
    assert.equal(ui.sidebarLinkIsActive("/admin", "/admin/settings"), false);
});

test("headerContextChangeRequiresRemount skips Store Information", function () {
    assert.equal(ui.headerContextChangeRequiresRemount("/admin/settings/store-information"), false);
    assert.equal(ui.headerContextChangeRequiresRemount("/admin/settings"), true);
    assert.equal(ui.headerContextChangeRequiresRemount("/admin/catalog/products"), true);
});

test("buildGeneralStorePutBody omits store.address and store.logo", function () {
    const body = ui.buildGeneralStorePutBody({
        code: "pl",
        name: "PL",
        domain: "ex.com",
        country: "PL",
        language: "pl",
        currency: "PLN",
        is_default: true,
        store_address: "should-not-appear",
        store_logo: "https://evil"
    });
    assert.deepEqual(Object.keys(body).sort(), [
        "code", "country", "currency", "domain", "is_default", "language", "name"
    ]);
    assert.equal(Object.prototype.hasOwnProperty.call(body, "store.address"), false);
    assert.equal(Object.prototype.hasOwnProperty.call(body, "store.logo"), false);
    assert.equal(body.is_default, true);
});

test("tax overwrite helpers", function () {
    const rows = [{ type: "vat_eu", value: "PL123" }, { type: "tin", value: "X" }];
    assert.equal(ui.existingTaxValue(rows, "vat_eu"), "PL123");
    assert.equal(ui.existingTaxValue(rows, "missing"), null);
    assert.equal(ui.taxSubmitLabel("PL123"), "Replace tax ID");
    assert.equal(ui.taxSubmitLabel(null), "Save tax ID");
    assert.equal(ui.shouldConfirmTaxOverwrite("PL123", "PL999"), true);
    assert.equal(ui.shouldConfirmTaxOverwrite("PL123", "PL123"), false);
    assert.equal(ui.shouldConfirmTaxOverwrite(null, "PL123"), false);
});

test("partitionTaxTypes keeps unfiltered list when country filter failed", function () {
    const all = [{ code: "vat_eu" }, { code: "tin" }, { code: "other" }];
    const failed = ui.partitionTaxTypes(all, [], true);
    assert.equal(failed.applicable.length, 3);
    assert.equal(failed.other.length, 0);

    const ok = ui.partitionTaxTypes(all, [{ code: "vat_eu" }], false);
    assert.equal(ok.applicable.length, 1);
    assert.equal(ok.other.length, 2);
});

test("scopeBannerCopy distinguishes empty vs unknown store id", function () {
    const empty = ui.scopeBannerCopy(null);
    assert.equal(empty.kind, "empty");
    assert.match(empty.text, /no global record/i);

    const unknown = ui.scopeBannerCopy(null, "store-uuid-missing");
    assert.equal(unknown.kind, "unknown");
    assert.match(unknown.text, /store-uuid-missing/);
    assert.match(unknown.text, /not in the store list/i);

    const store = ui.scopeBannerCopy({ name: "Main", country: "DE" });
    assert.equal(store.kind, "store");
    assert.match(store.text, /Main/);
    assert.match(store.text, /DE/);
    assert.match(store.text, /no global fallback/i);
});

test("composeSuccessReloadMessage keeps success text on type-list failure", function () {
    assert.equal(
        ui.composeSuccessReloadMessage("Tax identifier saved.", "types unavailable"),
        "Tax identifier saved. Reload failed: types unavailable"
    );
    assert.equal(
        ui.composeSuccessReloadMessage("", "boom"),
        "Reload failed: boom"
    );
});

test("admin.js wires Store Information route and omits address/logo config keys on General submit", function () {
    const adminSrc = fs.readFileSync(path.join(__dirname, "dist", "admin.js"), "utf8");
    const indexSrc = fs.readFileSync(path.join(__dirname, "dist", "index.html"), "utf8");

    assert.match(adminSrc, /\/admin\/settings\/store-information/);
    assert.match(adminSrc, /renderStoreInformationPage/);
    assert.match(indexSrc, /href="\/admin\/settings\/store-information"/);
    assert.match(indexSrc, /store_information_admin_ui\.js/);

    // General store submit must not PUT config entries for address/logo.
    assert.equal(adminSrc.indexOf("'store.address'"), -1);
    assert.equal(adminSrc.indexOf('"store.address"'), -1);
    assert.equal(adminSrc.indexOf("'store.logo'"), -1);
    assert.equal(adminSrc.indexOf('"store.logo"'), -1);
    assert.equal(adminSrc.indexOf("store_address"), -1);
    assert.equal(adminSrc.indexOf("store_logo"), -1);

    // Only one findStoreByID declaration (duplicate binding was a CR bug).
    const matches = adminSrc.match(/function findStoreByID\s*\(/g) || [];
    assert.equal(matches.length, 1);

    // Page uses gates, write-settlement, and DOM property apply paths.
    assert.match(adminSrc, /newStoreInfoLoadGate/);
    assert.match(adminSrc, /newTaxPanelLoadGate/);
    assert.match(adminSrc, /storeInfoTaxPanelGate/);
    assert.match(adminSrc, /afterWrite/);
    assert.match(adminSrc, /headerContextChangeRequiresRemount/);
    assert.match(adminSrc, /applyIdentityValues/);
    assert.match(adminSrc, /applyGeneralStoreValues/);
    assert.match(adminSrc, /sidebarLinkIsActive/);
    // refreshTaxIdentifiersPanel must await the type-list paint.
    assert.match(adminSrc, /return loadAndRenderTaxIdentifiersPanel/);
});
