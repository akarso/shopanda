"use strict";

const { test } = require("node:test");
const assert = require("node:assert/strict");
const ui = require("./dist/email_templates_admin_ui.js");

test("escAttr encodes quotes and angle brackets for attributes", function () {
    assert.equal(ui.escAttr('Thanks for your "order"'), "Thanks for your &quot;order&quot;");
    assert.equal(
        ui.escAttr('"><img src=x onerror=alert(1)>'),
        "&quot;&gt;&lt;img src=x onerror=alert(1)&gt;"
    );
    assert.equal(ui.escAttr(null), "");
});

test("editorShellHTML does not interpolate subject into attributes", function () {
    var html = ui.editorShellHTML({
        canWrite: true,
        templateWritable: true,
        hasContentWrite: true,
        selectedLocale: "",
        localeOptions: [""],
        metaHTML: "",
        bannerHTML: ""
    });
    assert.match(html, /id="email-template-subject" value=""/);
    assert.doesNotMatch(html, /onerror=/);
    assert.match(html, /referrerpolicy="no-referrer"/);
    assert.match(html, /saved<\/strong> render/i);
});

test("buildLocaleOptionsHTML uses escAttr for option values", function () {
    var html = ui.buildLocaleOptionsHTML(['"><x>'], '"><x>', function (s) { return s; });
    assert.match(html, /value="&quot;&gt;&lt;x&gt;"/);
});

test("normalizeLocaleInput canonicalizes primary subtag", function () {
    assert.deepEqual(ui.normalizeLocaleInput("pt-BR"), { ok: true, locale: "pt" });
    assert.deepEqual(ui.normalizeLocaleInput("DE"), { ok: true, locale: "de" });
    assert.deepEqual(ui.normalizeLocaleInput("und"), { ok: false, error: "invalid locale" });
    assert.deepEqual(ui.normalizeLocaleInput(""), { ok: true, locale: "" });
});

test("canShowWriteControls requires template writable AND content.write", function () {
    assert.equal(ui.canShowWriteControls(true, true), true);
    assert.equal(ui.canShowWriteControls(true, false), false);
    assert.equal(ui.canShowWriteControls(false, true), false);
});

test("listLoadResult fails closed on error or non-array", function () {
    assert.equal(ui.listLoadResult({ error: { code: "unavailable", message: "down" } }).ok, false);
    assert.equal(ui.listLoadResult({ error: { code: "forbidden" } }).forbidden, true);
    assert.equal(ui.listLoadResult({ data: null }).ok, false);
    assert.equal(ui.listLoadResult({ data: [] }).ok, true);
});

test("load gate ignores stale generations", function () {
    var gate = ui.newLoadGate();
    var first = gate.start();
    var second = gate.start();
    assert.equal(first.isCurrent(), false);
    assert.equal(second.isCurrent(), true);
    var painted = ui.paintPreview({
        meta: { textContent: "" },
        subject: { textContent: "" },
        frame: { setAttribute: function () {} }
    }, { subject: "stale" }, first);
    assert.equal(painted, false);
    painted = ui.paintPreview({
        meta: { textContent: "" },
        subject: { textContent: "" },
        frame: { attrs: {}, setAttribute: function (k, v) { this.attrs[k] = v; } }
    }, { subject: "fresh", body: "<p>ok</p>", source: "db" }, second);
    assert.equal(painted, true);
});

test("applyRowToFields sets DOM properties not attributes", function () {
    var subject = { value: "" };
    var body = { value: "" };
    var localeSelect = { value: "" };
    var baseline = ui.applyRowToFields(
        { subjectInput: subject, bodyInput: body, localeSelect: localeSelect },
        { locale: "de", subject: 'Thanks for your "order"', body: "<p>x</p>", source: "db" }
    );
    assert.equal(subject.value, 'Thanks for your "order"');
    assert.equal(body.value, "<p>x</p>");
    assert.equal(localeSelect.value, "de");
    assert.equal(baseline.subject, 'Thanks for your "order"');
});

test("formMatchesBaseline detects dirty textarea", function () {
    var baseline = ui.baselineFromRow({ locale: "", subject: "A", body: "B", source: "file" });
    assert.equal(ui.formMatchesBaseline("A", "B", "", baseline), true);
    assert.equal(ui.formMatchesBaseline("A!", "B", "", baseline), false);
});

test("writable false shell hides save/revert/test-send", function () {
    var html = ui.editorShellHTML({
        canWrite: false,
        templateWritable: false,
        hasContentWrite: true,
        selectedLocale: "",
        localeOptions: [""]
    });
    assert.doesNotMatch(html, /id="email-template-save"/);
    assert.doesNotMatch(html, /id="email-template-test-send"/);
    assert.match(html, /id="email-template-readonly-name"/);
});

test("content.read-only shell hides writes even when template writable", function () {
    var html = ui.editorShellHTML({
        canWrite: false,
        templateWritable: true,
        hasContentWrite: false,
        selectedLocale: "",
        localeOptions: [""]
    });
    assert.doesNotMatch(html, /id="email-template-save"/);
    assert.match(html, /id="email-template-readonly-perm"/);
});

test("defaultStoreBannerNeeded when header store differs", function () {
    assert.equal(ui.defaultStoreBannerNeeded("store-2", "store-1"), true);
    assert.equal(ui.defaultStoreBannerNeeded("store-1", "store-1"), false);
    assert.equal(ui.defaultStoreBannerNeeded("", "store-1"), false);
});

test("revertMessage distinguishes no-op delete", function () {
    assert.equal(ui.revertMessage(true), "Reverted to default.");
    assert.match(ui.revertMessage(false), /No override/);
});
