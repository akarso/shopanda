"use strict";

const { test } = require("node:test");
const assert = require("node:assert/strict");
const ui = require("./dist/cache_admin_ui.js");

function fakeEl(init) {
    var listeners = {};
    var attrs = {};
    var el = {
        disabled: false,
        hidden: false,
        value: "",
        textContent: "Clear",
        addEventListener: function (type, fn) {
            (listeners[type] = listeners[type] || []).push(fn);
        },
        dispatch: function (type, ev) {
            var list = listeners[type] || [];
            for (var i = 0; i < list.length; i++) {
                list[i](ev || { preventDefault: function () {} });
            }
        },
        setAttribute: function (name, value) {
            attrs[name] = String(value);
        },
        removeAttribute: function (name) {
            delete attrs[name];
        },
        getAttribute: function (name) {
            return Object.prototype.hasOwnProperty.call(attrs, name) ? attrs[name] : null;
        }
    };
    if (init) {
        Object.keys(init).forEach(function (k) {
            el[k] = init[k];
        });
    }
    return el;
}

function fakeForm() {
    return {
        form: fakeEl(),
        modeSelect: fakeEl({ value: "prefix" }),
        valueField: fakeEl(),
        valueInput: fakeEl({ value: "" }),
        confirmBox: fakeEl({ hidden: true }),
        confirmInput: fakeEl({ value: "" }),
        submitBtn: fakeEl({ disabled: false, textContent: "Clear" })
    };
}

function withBusyMatchingSPA(btn, busyLabel, startAction, afterRestore) {
    if (btn.disabled) {
        return Promise.resolve();
    }
    var original = btn.textContent;
    btn.disabled = true;
    btn.textContent = busyLabel;
    return Promise.resolve()
        .then(function () { return startAction(); })
        .then(function () {
            btn.disabled = false;
            btn.textContent = original;
            if (typeof afterRestore === "function") {
                afterRestore();
            }
        }, function (err) {
            btn.disabled = false;
            btn.textContent = original;
            if (typeof afterRestore === "function") {
                afterRestore();
            }
            return Promise.reject(err);
        });
}

test("modeOptions filters by cache.write and cache.clear_all", function () {
    assert.deepEqual(ui.modeOptions(true, false), ["prefix", "tag", "key"]);
    assert.deepEqual(ui.modeOptions(false, true), ["all"]);
    assert.deepEqual(ui.modeOptions(true, true), ["prefix", "tag", "key", "all"]);
    assert.deepEqual(ui.modeOptions(false, false), []);
});

test("submitDisabledForMode is the type-to-confirm gate", function () {
    assert.equal(ui.submitDisabledForMode("prefix", ""), false);
    assert.equal(ui.submitDisabledForMode("all", ""), true);
    assert.equal(ui.submitDisabledForMode("all", "CLEAR ALL"), false);
    assert.equal(ui.submitDisabledForMode("all", " clear all "), true);
    assert.equal(ui.submitDisabledForMode("all", "CLEAR ALL "), false);
});

test("buildPayload trims prefix and tag, keeps key whitespace", function () {
    assert.deepEqual(ui.buildPayload("prefix", "  product:1:  "), { ok: true, payload: { prefix: "product:1:" } });
    assert.deepEqual(ui.buildPayload("tag", "  cms:7  "), { ok: true, payload: { tag: "cms:7" } });
    assert.deepEqual(ui.buildPayload("key", "  mykey  "), { ok: true, payload: { key: "  mykey  " } });
    assert.deepEqual(ui.buildPayload("key", "   "), { ok: false, error: "empty" });
    assert.deepEqual(ui.buildPayload("all", "ignored"), { ok: true, payload: { all: true } });
});

test("normalizeL1 rejects a non-array", function () {
    assert.deepEqual(ui.normalizeL1(null), []);
    assert.equal(ui.normalizeL1({ name: "x" }), null);
    assert.deepEqual(ui.normalizeL1([{ name: "rbac.catalog" }]).length, 1);
});

test("Enter-submit while Clear is disabled does not call clear", function () {
    var els = fakeForm();
    els.modeSelect.value = "all";
    els.confirmInput.value = "";
    var cleared = 0;
    ui.bindClearForm(els, {
        withBusy: withBusyMatchingSPA,
        clear: function () {
            cleared++;
            return Promise.resolve({ message: "should not run" });
        },
        loadStats: function () { return Promise.resolve(); },
        onMessage: function () {}
    });
    assert.equal(els.submitBtn.disabled, true);
    els.form.dispatch("submit");
    assert.equal(cleared, 0);
});

test("successful all-clear leaves submit disabled after withBusy restore", async function () {
    var els = fakeForm();
    els.modeSelect.value = "all";
    els.confirmInput.value = "CLEAR ALL";
    var statsLoads = 0;
    var messages = [];
    var busyDone;
    ui.bindClearForm(els, {
        withBusy: function (btn, label, startAction, afterRestore) {
            busyDone = withBusyMatchingSPA(btn, label, startAction, afterRestore);
            return busyDone;
        },
        clear: function (payload) {
            assert.deepEqual(payload, { all: true });
            return Promise.resolve({ message: "Cleared all" });
        },
        loadStats: function () {
            statsLoads++;
            return Promise.resolve();
        },
        onMessage: function (kind, text) {
            messages.push({ kind: kind, text: text });
        }
    });
    assert.equal(els.submitBtn.disabled, false);
    els.form.dispatch("submit");
    await busyDone;
    assert.equal(els.confirmInput.value, "");
    assert.equal(els.submitBtn.disabled, true, "restore must not leave Clear enabled with an empty confirm");
    assert.equal(els.submitBtn.textContent, "Clear");
    assert.equal(els.modeSelect.disabled, false);
    assert.equal(els.confirmInput.disabled, false);
    assert.equal(statsLoads, 1);
    assert.equal(messages.some(function (m) { return m.kind === "ok"; }), true);
});

test("form fields are locked for the duration of clear", async function () {
    var els = fakeForm();
    els.modeSelect.value = "prefix";
    els.valueInput.value = " product:1: ";
    var release;
    var inflight = new Promise(function (resolve) { release = resolve; });
    var seenLocked = false;
    var busyDone;
    ui.bindClearForm(els, {
        withBusy: function (btn, label, startAction, afterRestore) {
            busyDone = withBusyMatchingSPA(btn, label, function () {
                seenLocked = els.modeSelect.disabled && els.valueInput.disabled && els.confirmInput.disabled;
                return startAction();
            }, afterRestore);
            return busyDone;
        },
        clear: function (payload) {
            assert.deepEqual(payload, { prefix: "product:1:" });
            return inflight.then(function () { return { message: "ok" }; });
        },
        loadStats: function () { return Promise.resolve(); },
        onMessage: function () {}
    });
    els.form.dispatch("submit");
    await Promise.resolve();
    assert.equal(seenLocked, true);
    assert.equal(els.modeSelect.disabled, true);
    assert.equal(els.modeSelect.getAttribute("aria-disabled"), "true");
    release();
    await busyDone;
    assert.equal(els.modeSelect.disabled, false);
    assert.equal(els.valueInput.value, "", "targeted value is cleared after success");
});

test("older stats response does not overwrite a newer one", async function () {
    var gate = ui.newStatsLoadGate();
    var shown = [];
    function load(p) {
        var req = gate.start();
        return p.then(function (v) {
            if (!req.isCurrent()) {
                return;
            }
            shown.push(v);
        });
    }
    var resolveOld;
    var resolveNew;
    var oldP = new Promise(function (resolve) { resolveOld = resolve; });
    var newP = new Promise(function (resolve) { resolveNew = resolve; });
    var pOld = load(oldP);
    var pNew = load(newP);
    resolveNew("after-clear");
    await pNew;
    resolveOld("before-clear");
    await pOld;
    assert.deepEqual(shown, ["after-clear"]);
});
