"use strict";

const { test } = require("node:test");
const assert = require("node:assert/strict");
const ui = require("./dist/product_visibility_section.js");

function makeLiveState(overrides) {
    return Object.assign({
        kind: "live",
        enforced: true,
        status: "active",
        inputFailureCodes: [],
        autoBasis: true,
        failureCodes: [],
        axes: {}
    }, overrides || {});
}

test("modesFromProduct reads PascalCase VisibilityAxes with auto fallback", () => {
    const product = { visibilityModes: { Catalog: "visible", Search: "hidden" } };
    assert.deepEqual(ui.modesFromProduct(product), {
        catalog: "visible",
        search: "hidden",
        individually: "auto",
        purchasable: "auto"
    });
});

test("modesFromProduct defaults every axis to auto when missing", () => {
    assert.deepEqual(ui.modesFromProduct(null), ui.defaultModes());
    assert.deepEqual(ui.modesFromProduct({}), ui.defaultModes());
});

test("stateFromVisibilityPayload maps the live payload shape", () => {
    const payload = {
        enforced: true,
        status: "draft",
        auto_basis: false,
        failure_codes: ["status_inactive"],
        axes: {
            catalog: { mode: "auto", override: false, resolved: false, effective: false, failure_codes: ["status_inactive"] },
            search: { mode: "auto", override: false, resolved: false, effective: false, failure_codes: ["status_inactive"] },
            individually: { mode: "auto", override: false, resolved: false, effective: false, failure_codes: ["status_inactive"] },
            purchasable: { mode: "hidden", override: true, resolved: false, effective: false }
        }
    };
    const state = ui.stateFromVisibilityPayload(payload);
    assert.equal(state.kind, "live");
    assert.equal(state.enforced, true);
    assert.equal(state.status, "draft");
    assert.equal(state.autoBasis, false);
    assert.deepEqual(state.failureCodes, ["status_inactive"]);
    assert.equal(state.axes.catalog.mode, "auto");
    assert.equal(state.axes.purchasable.override, true);
    assert.deepEqual(state.axes.purchasable.failureCodes, []);
});

test("stateFromVisibilityPayload returns null for create (no payload, no error)", () => {
    assert.equal(ui.stateFromVisibilityPayload(null), null);
    assert.equal(ui.stateFromVisibilityPayload(undefined), null);
    assert.equal(ui.stateFromVisibilityPayload(null, ""), null);
    assert.equal(ui.stateFromVisibilityPayload(undefined, undefined), null);
});

test("stateFromVisibilityPayload returns an error state for an existing product with visibility_error", () => {
    const state = ui.stateFromVisibilityPayload(null, "visibility facts unavailable");
    assert.deepEqual(state, { kind: "error", error: "visibility facts unavailable" });
});

test("stateFromVisibilityPayload prefers the live payload over a (theoretically impossible) sibling error", () => {
    const payload = {
        enforced: true, status: "active", auto_basis: true, failure_codes: [], axes: {}
    };
    const state = ui.stateFromVisibilityPayload(payload, "visibility facts unavailable");
    assert.equal(state.kind, "live");
});

// --- deriveAutoBasis / deriveFailureCodes ---

test("deriveAutoBasis: true only when active and no input failures", () => {
    assert.equal(ui.deriveAutoBasis("active", []), true);
    assert.equal(ui.deriveAutoBasis("active", undefined), true);
    assert.equal(ui.deriveAutoBasis("active", ["no_stock"]), false);
    assert.equal(ui.deriveAutoBasis("draft", []), false);
    assert.equal(ui.deriveAutoBasis("archived", []), false);
});

test("deriveFailureCodes: prepends status_inactive only when not active, never duplicates input codes", () => {
    assert.deepEqual(ui.deriveFailureCodes("active", []), []);
    assert.deepEqual(ui.deriveFailureCodes("active", ["no_stock"]), ["no_stock"]);
    assert.deepEqual(ui.deriveFailureCodes("draft", []), ["status_inactive"]);
    assert.deepEqual(ui.deriveFailureCodes("draft", ["no_stock"]), ["status_inactive", "no_stock"]);
    assert.deepEqual(ui.deriveFailureCodes("archived", undefined), ["status_inactive"]);
});

test("stateFromVisibilityPayload stores inputFailureCodes and derives autoBasis/failureCodes from it, not from auto_basis/failure_codes directly", () => {
    const payload = {
        enforced: true,
        status: "draft",
        auto_basis: false,
        failure_codes: ["status_inactive"],
        input_failure_codes: [],
        axes: {}
    };
    const state = ui.stateFromVisibilityPayload(payload);
    assert.deepEqual(state.inputFailureCodes, []);
    assert.equal(state.autoBasis, false);
    assert.deepEqual(state.failureCodes, ["status_inactive"]);
});

test("effectiveForMode: non-active status is never effective, even forced visible", () => {
    assert.equal(ui.effectiveForMode("visible", true, "draft"), false);
    assert.equal(ui.effectiveForMode("visible", true, "archived"), false);
    assert.equal(ui.effectiveForMode("auto", true, "draft"), false);
});

test("effectiveForMode: active status follows mode/autoBasis", () => {
    assert.equal(ui.effectiveForMode("auto", true, "active"), true);
    assert.equal(ui.effectiveForMode("auto", false, "active"), false);
    assert.equal(ui.effectiveForMode("visible", false, "active"), true);
    assert.equal(ui.effectiveForMode("hidden", true, "active"), false);
});

test("badgeText: create/null state shows mode label + computed after save", () => {
    assert.equal(ui.badgeText("auto", null), "Auto — computed after save");
    assert.equal(ui.badgeText("visible", null), "Visible — computed after save");
    assert.equal(ui.badgeText("hidden", null), "Hidden — computed after save");
});

test("badgeText: error state shows the same unavailable message for every mode", () => {
    const errorState = { kind: "error", error: "visibility facts unavailable" };
    assert.equal(ui.badgeText("auto", errorState), ui.ERROR_BADGE_TEXT);
    assert.equal(ui.badgeText("visible", errorState), ui.ERROR_BADGE_TEXT);
    assert.equal(ui.badgeText("hidden", errorState), ui.ERROR_BADGE_TEXT);
    assert.match(ui.ERROR_BADGE_TEXT, /unavailable/);
    assert.match(ui.ERROR_BADGE_TEXT, /reload to retry/);
    assert.doesNotMatch(ui.ERROR_BADGE_TEXT, /computed after save/);
});

test("badgeText: live + active, simplified auto/forced labels (no 'Would be' prefix)", () => {
    const active = makeLiveState({ status: "active", autoBasis: true });
    assert.equal(ui.badgeText("auto", active), "Auto — visible");
    assert.equal(ui.badgeText("auto", makeLiveState({ status: "active", autoBasis: false })), "Auto — hidden");
    assert.equal(ui.badgeText("visible", active), "Forced visible");
    assert.equal(ui.badgeText("hidden", active), "Forced hidden");
});

test("badgeText: forced visible on a non-active product is not effective", () => {
    const draft = makeLiveState({ status: "draft" });
    assert.equal(ui.badgeText("visible", draft), "Forced visible — not live (product inactive)");
});

test("badgeText: never prefixes 'Would be ...' even when !enforced (section caption handles it instead)", () => {
    const notEnforced = makeLiveState({ enforced: false, status: "active", autoBasis: false });
    assert.equal(ui.badgeText("auto", notEnforced), "Auto — hidden");
    assert.equal(ui.badgeText("visible", makeLiveState({ enforced: false, status: "active" })), "Forced visible");
    assert.equal(ui.badgeText("hidden", notEnforced), "Forced hidden");
    for (const mode of ui.MODES) {
        assert.doesNotMatch(ui.badgeText(mode, notEnforced), /Would be/);
    }
});

test("showFailures: only live + auto + autoBasis false", () => {
    assert.equal(ui.showFailures("auto", makeLiveState({ autoBasis: false })), true);
    assert.equal(ui.showFailures("auto", makeLiveState({ autoBasis: true })), false);
    assert.equal(ui.showFailures("visible", makeLiveState({ autoBasis: false })), false);
    assert.equal(ui.showFailures("hidden", makeLiveState({ autoBasis: false })), false);
    assert.equal(ui.showFailures("auto", null), false);
    assert.equal(ui.showFailures("auto", { kind: "error", error: "x" }), false);
});

test("failureLabel maps known codes and falls back to the raw code", () => {
    assert.equal(ui.failureLabel("status_inactive"), "status is not active");
    assert.equal(ui.failureLabel("no_stock"), "no variant in stock");
    assert.equal(ui.failureLabel("no_price"), "no price set");
    assert.equal(ui.failureLabel("no_store_assignment"), "no store assignment");
    assert.equal(ui.failureLabel("no_category"), "no category assignment");
    assert.equal(ui.failureLabel("requires_linked_parent"), "requires a linked parent");
    assert.equal(ui.failureLabel("something_new"), "something_new");
});

test("purchasable axis respects requiresLinkedParent even when mode is visible", () => {
    const state = makeLiveState({ autoBasis: true, requiresLinkedParent: true });
    assert.equal(ui.effectiveForMode("visible", true, "active", "purchasable", true), false);
    assert.equal(ui.effectiveForMode("auto", true, "active", "purchasable", true), false);
    assert.equal(ui.effectiveForMode("visible", true, "active", "catalog", true), true);
    assert.equal(ui.showFailures("visible", state, "purchasable"), true);
    assert.equal(ui.showFailures("visible", state, "catalog"), false);
    assert.deepEqual(ui.failureCodesFor(state, "purchasable"), ["requires_linked_parent"]);
    assert.match(ui.badgeText("visible", state, "purchasable"), /requires linked parent/);
    assert.equal(ui.badgeText("auto", state, "purchasable"), "Auto — hidden");
});

test("stateFromVisibilityPayload copies requires_linked_parent", () => {
    const state = ui.stateFromVisibilityPayload({
        enforced: true, status: "active", auto_basis: true, failure_codes: [],
        requires_linked_parent: true, axes: {}
    });
    assert.equal(state.requiresLinkedParent, true);
});

// FAILURE_LABELS key sync with Go is covered by TestFailureLabelsMatchAutoBasisCodes
// in product_visibility_section_node_test.go.

test("escapeHTML escapes &, <, >, \", and '", () => {
    assert.equal(ui.escapeHTML(`<b>"it's" & more</b>`), "&lt;b&gt;&quot;it&#39;s&quot; &amp; more&lt;/b&gt;");
});

test("renderOptionsHTML marks the selected mode", () => {
    const html = ui.renderOptionsHTML("hidden");
    assert.match(html, /<option value="auto">auto<\/option>/);
    assert.match(html, /<option value="hidden" selected>hidden<\/option>/);
});

test("renderRowHTML includes failures list only when live + auto + autoBasis false", () => {
    const axis = ui.AXES[0];
    const hiddenState = makeLiveState({ autoBasis: false, failureCodes: ["no_price"] });
    const shown = ui.renderRowHTML(axis, "auto", hiddenState);
    assert.match(shown, /no price set/);
    assert.doesNotMatch(shown, /display:none;/);

    const visibleState = makeLiveState({ autoBasis: true, failureCodes: ["no_price"] });
    const hiddenList = ui.renderRowHTML(axis, "auto", visibleState);
    assert.match(hiddenList, /style="display:none;"/);

    const forced = ui.renderRowHTML(axis, "visible", hiddenState);
    assert.match(forced, /style="display:none;"/);
    assert.match(forced, /Forced visible/);
});

test("renderRowHTML: error state shows the unavailable badge and no failures list content", () => {
    const axis = ui.AXES[0];
    const errorState = { kind: "error", error: "visibility facts unavailable" };
    const html = ui.renderRowHTML(axis, "auto", errorState);
    assert.match(html, new RegExp(ui.ERROR_BADGE_TEXT.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")));
    assert.match(html, /style="display:none;"/);
});

test("renderSectionHTML renders all four axes", () => {
    const html = ui.renderSectionHTML(ui.defaultModes(), null);
    for (const axis of ui.AXES) {
        assert.match(html, new RegExp(ui.selectID(axis.key)));
    }
});

test("renderSectionHTML: enforcement-off caption appears once at section level, not per badge", () => {
    const notEnforced = makeLiveState({ enforced: false, status: "active", autoBasis: false });
    const html = ui.renderSectionHTML(ui.defaultModes(), notEnforced);
    const occurrences = html.split(ui.ENFORCEMENT_OFF_CAPTION).length - 1;
    assert.equal(occurrences, 1, "caption should appear exactly once, not per axis badge");
    assert.doesNotMatch(html, /Would be/);
});

test("renderSectionHTML: status-freshness caption appears for live state, enforced or not", () => {
    const enforced = makeLiveState({ enforced: true });
    const notEnforced = makeLiveState({ enforced: false });
    assert.match(ui.renderSectionHTML(ui.defaultModes(), enforced), new RegExp(ui.STATUS_FRESHNESS_CAPTION.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")));
    assert.match(ui.renderSectionHTML(ui.defaultModes(), notEnforced), new RegExp(ui.STATUS_FRESHNESS_CAPTION.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")));
});

test("renderSectionHTML: no captions for create/null or error state", () => {
    const nullHTML = ui.renderSectionHTML(ui.defaultModes(), null);
    assert.doesNotMatch(nullHTML, /product-visibility-caption/);
    const errorHTML = ui.renderSectionHTML(ui.defaultModes(), { kind: "error", error: "x" });
    assert.doesNotMatch(errorHTML, /product-visibility-caption/);
});

test("collectModes reads each select's current value, defaulting to auto", () => {
    const values = { "product-visibility-mode-catalog": "visible" };
    const doc = {
        getElementById: (id) => (id in values ? { value: values[id] } : null)
    };
    assert.deepEqual(ui.collectModes(doc), {
        catalog: "visible",
        search: "auto",
        individually: "auto",
        purchasable: "auto"
    });
});

test("updateRowFromMode: forced visible -> auto when autoBasis is false shows Auto — hidden with failures", () => {
    const badge = { textContent: "" };
    const list = { style: { display: "" } };
    const doc = {
        getElementById: (id) => {
            if (id === ui.badgeID("catalog")) return badge;
            if (id === ui.failuresID("catalog")) return list;
            return null;
        }
    };
    const state = makeLiveState({ status: "active", autoBasis: false, failureCodes: ["no_price"] });

    ui.updateRowFromMode(doc, ui.AXES[0], "auto", state);
    assert.equal(badge.textContent, "Auto — hidden");
    assert.equal(list.style.display, "");
});

test("updateRowFromMode: forced hidden -> auto when autoBasis is true shows Auto — visible with failures hidden", () => {
    const badge = { textContent: "" };
    const list = { style: { display: "" } };
    const doc = {
        getElementById: (id) => {
            if (id === ui.badgeID("catalog")) return badge;
            if (id === ui.failuresID("catalog")) return list;
            return null;
        }
    };
    const state = makeLiveState({ status: "active", autoBasis: true });

    ui.updateRowFromMode(doc, ui.AXES[0], "auto", state);
    assert.equal(badge.textContent, "Auto — visible");
    assert.equal(list.style.display, "none");
});

test("updateRowFromMode: auto -> forced visible on a draft product shows the not-live label", () => {
    const badge = { textContent: "" };
    const list = { style: { display: "" } };
    const doc = {
        getElementById: (id) => {
            if (id === ui.badgeID("catalog")) return badge;
            if (id === ui.failuresID("catalog")) return list;
            return null;
        }
    };
    const state = makeLiveState({ status: "draft", autoBasis: false, failureCodes: ["status_inactive"] });

    ui.updateRowFromMode(doc, ui.AXES[0], "visible", state);
    assert.equal(badge.textContent, "Forced visible — not live (product inactive)");
    assert.equal(list.style.display, "none");
});

test("updateRowFromMode: create/null state always shows computed-after-save regardless of mode", () => {
    const badge = { textContent: "" };
    const list = { style: { display: "" } };
    const doc = {
        getElementById: (id) => {
            if (id === ui.badgeID("catalog")) return badge;
            if (id === ui.failuresID("catalog")) return list;
            return null;
        }
    };

    ui.updateRowFromMode(doc, ui.AXES[0], "hidden", null);
    assert.equal(badge.textContent, "Hidden — computed after save");
    assert.equal(list.style.display, "none");
});

test("updateRowFromMode: error state shows the unavailable message for every mode, no failures", () => {
    const badge = { textContent: "" };
    const list = { style: { display: "" } };
    const doc = {
        getElementById: (id) => {
            if (id === ui.badgeID("catalog")) return badge;
            if (id === ui.failuresID("catalog")) return list;
            return null;
        }
    };
    const state = { kind: "error", error: "visibility facts unavailable" };

    ui.updateRowFromMode(doc, ui.AXES[0], "auto", state);
    assert.equal(badge.textContent, ui.ERROR_BADGE_TEXT);
    assert.equal(list.style.display, "none");

    ui.updateRowFromMode(doc, ui.AXES[0], "visible", state);
    assert.equal(badge.textContent, ui.ERROR_BADGE_TEXT);
});

test("bindAxisChange wires a change listener per axis exactly once", () => {
    let handlers = {};
    function makeSelect(id) {
        return {
            id: id,
            value: "auto",
            attrs: {},
            getAttribute: function (name) { return this.attrs[name]; },
            setAttribute: function (name, v) { this.attrs[name] = v; },
            addEventListener: function (evt, fn) {
                handlers[id] = handlers[id] || [];
                handlers[id].push(fn);
            }
        };
    }
    const elements = {};
    for (const axis of ui.AXES) {
        elements[ui.selectID(axis.key)] = makeSelect(ui.selectID(axis.key));
        elements[ui.badgeID(axis.key)] = { textContent: "" };
        elements[ui.failuresID(axis.key)] = { style: { display: "" } };
    }
    const doc = { getElementById: (id) => elements[id] || null };
    const state = makeLiveState({ status: "active", autoBasis: false, failureCodes: ["no_price"] });

    ui.bindAxisChange(doc, state);
    ui.bindAxisChange(doc, state);

    assert.equal(handlers[ui.selectID("catalog")].length, 1, "listener should only bind once");

    elements[ui.selectID("catalog")].value = "visible";
    handlers[ui.selectID("catalog")][0]();
    assert.equal(elements[ui.badgeID("catalog")].textContent, "Forced visible");
});

// --- Status freshness ---

function makeStatusSelect() {
    return {
        value: "draft",
        attrs: {},
        handlers: [],
        getAttribute: function (name) { return this.attrs[name]; },
        setAttribute: function (name, v) { this.attrs[name] = v; },
        addEventListener: function (evt, fn) { this.handlers.push(fn); }
    };
}

function makeAxisDoc(initialMode, axisKey) {
    const axis = axisKey || "catalog";
    const badge = { textContent: "" };
    const list = { style: { display: "" }, innerHTML: "" };
    const modeSelect = { value: initialMode || "auto" };
    const doc = {
        getElementById: (id) => {
            if (id === ui.selectID(axis)) return modeSelect;
            if (id === ui.badgeID(axis)) return badge;
            if (id === ui.failuresID(axis)) return list;
            return null;
        }
    };
    return { doc, badge, list, modeSelect };
}

function makeRequiresLinkedCheckbox(checked) {
    return {
        checked: !!checked,
        attrs: {},
        handlers: [],
        getAttribute: function (name) { return this.attrs[name]; },
        setAttribute: function (name, v) { this.attrs[name] = v; },
        addEventListener: function (evt, fn) { this.handlers.push(fn); }
    };
}

test("bindStatusChange: selecting a different form status recomputes the auto badge", () => {
    const { doc, badge, modeSelect } = makeAxisDoc("auto");
    const statusEl = makeStatusSelect();
    const form = { elements: { status: statusEl } };
    const state = makeLiveState({ status: "draft", autoBasis: true });

    ui.bindStatusChange(doc, form, state);
    assert.equal(statusEl.handlers.length, 1);

    // Still draft -> auto is never effective regardless of autoBasis.
    assert.equal(ui.badgeText("auto", state), "Auto — hidden");

    statusEl.value = "active";
    statusEl.handlers[0]();

    assert.equal(state.status, "active");
    assert.equal(modeSelect.value, "auto");
    assert.equal(badge.textContent, "Auto — visible");
});

test("bindStatusChange: recomputes using the currently selected mode select, not just auto", () => {
    const { doc, badge, modeSelect } = makeAxisDoc("visible");
    const statusEl = makeStatusSelect();
    statusEl.value = "draft";
    const form = { elements: { status: statusEl } };
    const state = makeLiveState({ status: "draft", autoBasis: false });

    ui.bindStatusChange(doc, form, state);
    modeSelect.value = "visible";
    statusEl.value = "active";
    statusEl.handlers[0]();

    assert.equal(state.status, "active");
    assert.equal(badge.textContent, "Forced visible");
});

test("bindStatusChange: binds only once even if called twice", () => {
    const { doc } = makeAxisDoc("auto");
    const statusEl = makeStatusSelect();
    const form = { elements: { status: statusEl } };
    const state = makeLiveState();

    ui.bindStatusChange(doc, form, state);
    ui.bindStatusChange(doc, form, state);

    assert.equal(statusEl.handlers.length, 1);
});

test("bindStatusChange: no-ops for create/null and error state (nothing to preview)", () => {
    const { doc } = makeAxisDoc("auto");
    const statusEl = makeStatusSelect();
    const form = { elements: { status: statusEl } };

    ui.bindStatusChange(doc, form, null);
    ui.bindStatusChange(doc, form, { kind: "error", error: "x" });

    assert.equal(statusEl.handlers.length, 0);
});

test("bindStatusChange: no-ops without a form or without form.elements", () => {
    const { doc } = makeAxisDoc("auto");
    const state = makeLiveState();
    // Should not throw.
    ui.bindStatusChange(doc, null, state);
    ui.bindStatusChange(doc, {}, state);
});

// --- refreshLivePreview (facts refresh without resetting modes) ---

test("refreshLivePreview: clears no_price input failure, active + auto -> Auto — visible, failures empty, mode select unchanged", () => {
    const { doc, badge, list, modeSelect } = makeAxisDoc("auto");
    const form = { elements: { status: { value: "active" } } };
    const state = makeLiveState({
        status: "active",
        inputFailureCodes: ["no_price"],
        autoBasis: false,
        failureCodes: ["no_price"]
    });

    ui.refreshLivePreview(doc, form, state, []);

    assert.equal(modeSelect.value, "auto");
    assert.deepEqual(state.inputFailureCodes, []);
    assert.equal(state.autoBasis, true);
    assert.deepEqual(state.failureCodes, []);
    assert.equal(badge.textContent, "Auto — visible");
    assert.equal(list.innerHTML, "");
    assert.equal(list.style.display, "none");
});

test("refreshLivePreview: no-ops for null, error, and pending state", () => {
    const { doc, badge } = makeAxisDoc("auto");
    const form = { elements: { status: { value: "active" } } };
    const errorState = { kind: "error", error: "x" };

    ui.refreshLivePreview(doc, form, null, []);
    ui.refreshLivePreview(doc, form, errorState, []);
    assert.equal(badge.textContent, "");
});

test("refreshLivePreview: reads status from form when present", () => {
    const { doc, badge } = makeAxisDoc("visible");
    const form = { elements: { status: { value: "draft" } } };
    const state = makeLiveState({ status: "active", inputFailureCodes: [], autoBasis: true });

    ui.refreshLivePreview(doc, form, state, []);

    assert.equal(state.status, "draft");
    assert.equal(badge.textContent, "Forced visible — not live (product inactive)");
});

test("refreshLivePreview: reads requires_linked_parent from form checkbox", () => {
    const { doc, badge, list } = makeAxisDoc("visible", "purchasable");
    const form = {
        elements: {
            status: { value: "active" },
            requires_linked_parent: { checked: true }
        }
    };
    const state = makeLiveState({
        status: "active",
        requiresLinkedParent: false,
        inputFailureCodes: [],
        autoBasis: true
    });

    ui.refreshLivePreview(doc, form, state, []);

    assert.equal(state.requiresLinkedParent, true);
    assert.equal(badge.textContent, "Forced visible — not purchasable (requires linked parent)");
    assert.equal(list.style.display, "");
    assert.match(list.innerHTML, /requires a linked parent/);
});

test("bindRequiresLinkedParentChange: toggling checkbox recomputes purchasable badge", () => {
    const { doc, badge } = makeAxisDoc("visible", "purchasable");
    const checkbox = makeRequiresLinkedCheckbox(false);
    const form = {
        elements: {
            status: { value: "active" },
            requires_linked_parent: checkbox
        }
    };
    const state = makeLiveState({ status: "active", requiresLinkedParent: false, autoBasis: true });

    ui.bindRequiresLinkedParentChange(doc, form, state);
    assert.equal(checkbox.handlers.length, 1);

    checkbox.checked = true;
    checkbox.handlers[0]();

    assert.equal(state.requiresLinkedParent, true);
    assert.equal(badge.textContent, "Forced visible — not purchasable (requires linked parent)");
});

test("bindRequiresLinkedParentChange: binds only once even if called twice", () => {
    const { doc } = makeAxisDoc("visible", "purchasable");
    const checkbox = makeRequiresLinkedCheckbox(false);
    const form = { elements: { requires_linked_parent: checkbox } };
    const state = makeLiveState();

    ui.bindRequiresLinkedParentChange(doc, form, state);
    ui.bindRequiresLinkedParentChange(doc, form, state);

    assert.equal(checkbox.handlers.length, 1);
});

test("bindRequiresLinkedParentChange: no-ops for create/null and error state", () => {
    const { doc } = makeAxisDoc("visible", "purchasable");
    const checkbox = makeRequiresLinkedCheckbox(false);
    const form = { elements: { requires_linked_parent: checkbox } };

    ui.bindRequiresLinkedParentChange(doc, form, null);
    ui.bindRequiresLinkedParentChange(doc, form, { kind: "error", error: "x" });

    assert.equal(checkbox.handlers.length, 0);
});

// --- Live status preview via input_failure_codes ---

test("status preview 1: draft -> active when only status_inactive was failing — autoBasis true, badge Auto — visible, failures empty", () => {
    const { doc, badge, list, modeSelect } = makeAxisDoc("auto");
    const statusEl = makeStatusSelect();
    statusEl.value = "draft";
    const form = { elements: { status: statusEl } };
    // Only status was failing: inputFailureCodes (non-status) is empty.
    const state = makeLiveState({ status: "draft", inputFailureCodes: [], autoBasis: false, failureCodes: ["status_inactive"] });

    ui.bindStatusChange(doc, form, state);
    modeSelect.value = "auto";

    statusEl.value = "active";
    statusEl.handlers[0]();

    assert.equal(state.autoBasis, true);
    assert.deepEqual(state.failureCodes, []);
    assert.equal(badge.textContent, "Auto — visible");
    assert.equal(list.innerHTML, "");
    assert.equal(list.style.display, "none");
});

test("status preview 2: active -> draft when inputs all ok — autoBasis false, badge Auto — hidden, failures show status_inactive", () => {
    const { doc, badge, list, modeSelect } = makeAxisDoc("auto");
    const statusEl = makeStatusSelect();
    statusEl.value = "active";
    const form = { elements: { status: statusEl } };
    const state = makeLiveState({ status: "active", inputFailureCodes: [], autoBasis: true, failureCodes: [] });

    ui.bindStatusChange(doc, form, state);
    modeSelect.value = "auto";

    statusEl.value = "draft";
    statusEl.handlers[0]();

    assert.equal(state.autoBasis, false);
    assert.deepEqual(state.failureCodes, ["status_inactive"]);
    assert.equal(badge.textContent, "Auto — hidden");
    assert.match(list.innerHTML, /status is not active/);
    assert.equal(list.style.display, "");
});

test("status preview 3: draft -> active when also no_stock — still hidden, failures list has no_stock only (no status_inactive)", () => {
    const { doc, badge, list, modeSelect } = makeAxisDoc("auto");
    const statusEl = makeStatusSelect();
    statusEl.value = "draft";
    const form = { elements: { status: statusEl } };
    // no_stock is a non-status input failure — present regardless of status.
    const state = makeLiveState({ status: "draft", inputFailureCodes: ["no_stock"], autoBasis: false, failureCodes: ["status_inactive", "no_stock"] });

    ui.bindStatusChange(doc, form, state);
    modeSelect.value = "auto";

    statusEl.value = "active";
    statusEl.handlers[0]();

    assert.equal(state.autoBasis, false);
    assert.deepEqual(state.failureCodes, ["no_stock"]);
    assert.equal(badge.textContent, "Auto — hidden");
    assert.equal(list.style.display, "");
    assert.match(list.innerHTML, /no variant in stock/);
    assert.doesNotMatch(list.innerHTML, /status is not active/);
});
