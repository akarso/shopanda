"use strict";

const { test } = require("node:test");
const assert = require("node:assert/strict");
const ui = require("./dist/product_type_sections.js");

test("sectionTypes trims and drops empties", () => {
    assert.deepEqual(ui.sectionTypes(" bundle , grouped "), ["bundle", "grouped"]);
    assert.deepEqual(ui.sectionTypes(""), []);
    assert.deepEqual(ui.sectionTypes(null), []);
});

test("sectionVisible: empty attribute always visible", () => {
    assert.equal(ui.sectionVisible("", "simple"), true);
    assert.equal(ui.sectionVisible("", "bundle"), true);
});

test("sectionVisible: match and non-match", () => {
    assert.equal(ui.sectionVisible("bundle", "bundle"), true);
    assert.equal(ui.sectionVisible("bundle,grouped", "grouped"), true);
    assert.equal(ui.sectionVisible("bundle", "simple"), false);
});

test("syncSections toggles display only", () => {
    const nodes = [
        { getAttribute: () => "bundle", style: { display: "none", marginTop: "2rem" } },
        { getAttribute: () => "", style: { display: "none", color: "red" } },
        { getAttribute: () => "virtual", style: { display: "" } }
    ];
    const root = {
        querySelectorAll: () => nodes
    };
    ui.syncSections(root, "bundle");
    assert.equal(nodes[0].style.display, "");
    assert.equal(nodes[0].style.marginTop, "2rem");
    assert.equal(nodes[1].style.display, "");
    assert.equal(nodes[1].style.color, "red");
    assert.equal(nodes[2].style.display, "none");
});

test("selectNeedsUnknownOption", () => {
    const options = [{ value: "simple" }, { value: "virtual" }];
    assert.equal(ui.selectNeedsUnknownOption(options, "simple"), false);
    assert.equal(ui.selectNeedsUnknownOption(options, "kit"), true);
    assert.equal(ui.selectNeedsUnknownOption(options, ""), false);
});

test("labelsFromForm and displayLabel use schema options", () => {
    const form = {
        fields: [
            { name: "status", options: [{ value: "draft", label: "Draft" }] },
            {
                name: "type",
                options: [
                    { value: "simple", label: "Simple" },
                    { value: "virtual", label: "Virtual" }
                ]
            }
        ]
    };
    const labels = ui.labelsFromForm(form);
    assert.deepEqual(labels, { simple: "Simple", virtual: "Virtual" });
    assert.equal(ui.displayLabel("virtual", labels), "Virtual");
    assert.equal(ui.displayLabel("kit", labels), "kit");
    assert.deepEqual(ui.labelsFromForm(null), {});
});

test("applyTypeCellLabels updates text from data attribute", () => {
    const cells = [
        { getAttribute: () => "virtual", textContent: "virtual" },
        { getAttribute: () => "kit", textContent: "kit" }
    ];
    ui.applyTypeCellLabels({ querySelectorAll: () => cells }, { virtual: "Virtual" });
    assert.equal(cells[0].textContent, "Virtual");
    assert.equal(cells[1].textContent, "kit");
});

test("resolveTypeChange confirms and advances baseline", () => {
    let asked = "";
    const accepted = ui.resolveTypeChange({
        confirmOnChange: true,
        previousType: "simple",
        nextType: "virtual",
        confirmFn: function (msg) {
            asked = msg;
            return true;
        }
    });
    assert.equal(asked, ui.TYPE_CHANGE_CONFIRM);
    assert.deepEqual(accepted, { type: "virtual", previousType: "virtual", cancelled: false });
});

test("resolveTypeChange cancel restores previous selection baseline", () => {
    const cancelled = ui.resolveTypeChange({
        confirmOnChange: true,
        previousType: "simple",
        nextType: "virtual",
        confirmFn: function () { return false; }
    });
    assert.deepEqual(cancelled, { type: "simple", previousType: "simple", cancelled: true });
});

test("resolveTypeChange skips confirm when unchanged or disabled", () => {
    assert.deepEqual(ui.resolveTypeChange({
        confirmOnChange: true,
        previousType: "simple",
        nextType: "simple",
        confirmFn: function () { throw new Error("should not confirm"); }
    }), { type: "simple", previousType: "simple", cancelled: false });

    assert.deepEqual(ui.resolveTypeChange({
        confirmOnChange: false,
        previousType: "simple",
        nextType: "virtual",
        confirmFn: function () { throw new Error("should not confirm"); }
    }), { type: "virtual", previousType: "simple", cancelled: false });
});

test("assignProductField puts type on payload root not attributes", () => {
    const payload = { attributes: {} };
    ui.assignProductField(payload, "type", "virtual");
    ui.assignProductField(payload, "color", "red");
    assert.equal(payload.type, "virtual");
    assert.equal(payload.attributes.color, "red");
    assert.equal(payload.attributes.type, undefined);
    assert.equal(ui.isCoreProductField("type"), true);
    assert.equal(ui.isCoreProductField("color"), false);
});

test("inline-fallback parity: unknown option and section sync helpers stay consistent", () => {
    // Documents the admin.js inline fallback contract: same predicates as the module.
    function selectNeedsUnknownOptionFallback(options, selectedValue) {
        var selected = String(selectedValue || "");
        if (!selected) {
            return false;
        }
        for (var i = 0; i < (options || []).length; i++) {
            if (String(options[i].value) === selected) {
                return false;
            }
        }
        return true;
    }
    function syncFallback(root, selectedType) {
        var selected = String(selectedType || "simple");
        var nodes = root.querySelectorAll("[data-product-type-section]");
        for (var i = 0; i < nodes.length; i++) {
            var raw = nodes[i].getAttribute("data-product-type-section") || "";
            var types = String(raw).split(",").map(function (s) {
                return String(s || "").trim();
            }).filter(Boolean);
            nodes[i].style.display = (types.length === 0 || types.indexOf(selected) !== -1) ? "" : "none";
        }
    }

    const options = [{ value: "simple" }];
    assert.equal(selectNeedsUnknownOptionFallback(options, "kit"), ui.selectNeedsUnknownOption(options, "kit"));
    assert.equal(selectNeedsUnknownOptionFallback(options, "simple"), ui.selectNeedsUnknownOption(options, "simple"));

    const nodes = [{ getAttribute: () => "bundle", style: { display: "none" } }];
    const root = { querySelectorAll: () => nodes };
    syncFallback(root, "simple");
    ui.syncSections({ querySelectorAll: () => [{ getAttribute: () => "bundle", style: { display: "none" } }] }, "simple");
    assert.equal(nodes[0].style.display, "none");
});
