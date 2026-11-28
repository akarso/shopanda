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

test("editorShellHTML keeps add-locale and test-send outside the save form", function () {
    var html = ui.editorShellHTML({
        canWrite: true,
        templateWritable: true,
        hasContentWrite: true,
        selectedLocale: "",
        localeOptions: [""]
    });
    var formStart = html.indexOf('<form id="email-template-form">');
    var formEnd = html.indexOf("</form>", formStart);
    assert.ok(formStart >= 0 && formEnd > formStart);
    var formHTML = html.slice(formStart, formEnd);
    assert.doesNotMatch(formHTML, /email-template-new-locale/);
    assert.doesNotMatch(formHTML, /email-template-test-to/);
    assert.doesNotMatch(formHTML, /email-template-test-send/);
    assert.match(formHTML, /email-template-save/);
    assert.match(html, /id="email-template-new-locale"/);
    assert.match(html, /id="email-template-test-fieldset"/);
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

function fakeEditorRoot(connected) {
    var store = {};
    var root = {
        isConnected: connected !== false,
        querySelector: function (sel) {
            var id = String(sel || "").replace(/^#/, "");
            return store[id] || null;
        },
        setField: function (id, el) {
            store[id] = el;
        }
    };
    return root;
}

test("preview begin does not retire locale lock; stale unlock only for lock owner", function () {
    var editor = fakeEditorRoot(true);
    var session = ui.createScopedEditorSession(editor);
    var localeTicket = session.beginLocaleLoad();
    session.beginPreview(); // must not make locale ticket stale
    assert.equal(localeTicket.isCurrent(), true);
    assert.equal(session.settleLocaleLoad(localeTicket), "apply");

    var session2 = ui.createScopedEditorSession(editor);
    var first = session2.beginLocaleLoad();
    var second = session2.beginLocaleLoad();
    assert.equal(session2.settleLocaleLoad(first), "stale-ignore");
    assert.equal(session2.settleLocaleLoad(second), "apply");
});

test("detached editor root settles as detached and does not apply into another root", function () {
    var rootA = fakeEditorRoot(true);
    var rootB = fakeEditorRoot(true);
    var subjectA = { value: "A-old" };
    var subjectB = { value: "B-old" };
    rootA.setField("email-template-subject", subjectA);
    rootB.setField("email-template-subject", subjectB);

    var sessionA = ui.createScopedEditorSession(rootA);
    var ticket = sessionA.beginLocaleLoad();
    rootA.isConnected = false;

    assert.equal(sessionA.settleLocaleLoad(ticket), "detached");
    // Writing through sessionA.q still targets A's detached field, never B.
    var fieldA = sessionA.q("email-template-subject");
    fieldA.value = "from-A-late";
    assert.equal(subjectA.value, "from-A-late");
    assert.equal(subjectB.value, "B-old");
});

test("write settle ignores responses after locale load starts", function () {
    var editor = fakeEditorRoot(true);
    var session = ui.createScopedEditorSession(editor);
    var writeTicket = session.beginWrite();
    session.beginLocaleLoad(); // bumps write gate
    assert.equal(session.settleWrite(writeTicket), "stale");
});

test("side action (test-send) is invalidated by locale load", function () {
    var editor = fakeEditorRoot(true);
    var session = ui.createScopedEditorSession(editor);
    var side = session.beginSideAction();
    session.beginLocaleLoad();
    assert.equal(session.settleSideAction(side), "stale");
    assert.equal(session.settleSideAction(session.beginSideAction()), "apply");
});

test("sideActionCompletion refreshes controls on stale but not detached", function () {
    assert.deepEqual(ui.sideActionCompletion("stale"), {
        clearInFlight: true,
        syncControls: true,
        paintStatus: false
    });
    assert.deepEqual(ui.sideActionCompletion("detached"), {
        clearInFlight: true,
        syncControls: false,
        paintStatus: false
    });
    assert.deepEqual(ui.sideActionCompletion("apply"), {
        clearInFlight: true,
        syncControls: true,
        paintStatus: true
    });
});

test("stale test-send after locale apply re-enables Send test", function () {
    var editor = fakeEditorRoot(true);
    var testBtn = { disabled: true };
    editor.setField("email-template-test-send", testBtn);
    editor.setField("email-template-subject", { value: "Hi", dataset: { ro: "0" }, disabled: false });
    editor.setField("email-template-body", { value: "<p>x</p>", dataset: { ro: "0" }, disabled: false });
    editor.setField("email-template-locale", { value: "de", disabled: false, innerHTML: "" });

    var session = ui.createScopedEditorSession(editor);
    var sideTicket = session.beginSideAction();
    var testSendInFlight = true;
    var baseline = ui.baselineFromRow({
        locale: "de",
        subject: "Hi",
        body: "<p>x</p>",
        source: "db"
    });

    // Locale GET finishes first while SMTP is still in flight.
    session.beginLocaleLoad();
    ui.applyRowToFields({
        subjectInput: editor.querySelector("#email-template-subject"),
        bodyInput: editor.querySelector("#email-template-body"),
        localeSelect: editor.querySelector("#email-template-locale")
    }, baseline);
    // Simulate applyLoadedRow unlock while in-flight flag is still true.
    ui.setWriteControlsLocked({
        testBtn: testBtn,
        localeSelect: editor.querySelector("#email-template-locale"),
        subjectInput: editor.querySelector("#email-template-subject"),
        bodyInput: editor.querySelector("#email-template-body")
    }, false);
    ui.setTestSendEnabled(testBtn, !testSendInFlight);
    assert.equal(testBtn.disabled, true);

    // Late SMTP response: stale side settle must clear the flag and refresh.
    var outcome = session.settleSideAction(sideTicket);
    assert.equal(outcome, "stale");
    var plan = ui.sideActionCompletion(outcome);
    if (plan.clearInFlight) {
        testSendInFlight = false;
    }
    if (plan.syncControls) {
        var clean = ui.formMatchesBaseline("Hi", "<p>x</p>", "de", baseline);
        ui.setTestSendEnabled(testBtn, clean && !testSendInFlight);
    }
    assert.equal(testSendInFlight, false);
    assert.equal(testBtn.disabled, false);
});
