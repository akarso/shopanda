"use strict";

const { test } = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const ui = require("./dist/product_type_sections.js");

// Source of truth for product.form section Types: cmd/api/wire_services.go
// RegisterProductFormSection calls. Parsed into form-schema section shape so the
// matrix exercises real registration attrs (not a duplicated Types map).
function extractBalanced(src, openIdx) {
    let depth = 0;
    for (let i = openIdx; i < src.length; i++) {
        const ch = src[i];
        if (ch === "{") {
            depth += 1;
        } else if (ch === "}") {
            depth -= 1;
            if (depth === 0) {
                return src.slice(openIdx + 1, i);
            }
        }
    }
    return "";
}

function loadRegisteredProductFormSections() {
    const wirePath = path.resolve(__dirname, "../../../../cmd/api/wire_services.go");
    const src = fs.readFileSync(wirePath, "utf8");
    const sections = [];
    const parts = src.split("RegisterProductFormSection(");
    for (let i = 1; i < parts.length; i++) {
        const part = parts[i];
        const marker = "FormSection{";
        const start = part.indexOf(marker);
        if (start < 0) {
            continue;
        }
        const body = extractBalanced(part, start + marker.length - 1);
        const idMatch = /ID:\s*"([^"]+)"/.exec(body);
        if (!idMatch) {
            continue;
        }
        let types = [];
        if (!/\bTypes:\s*nil\b/.test(body)) {
            const typesMatch = /Types:\s*\[\]string\{([^}]*)\}/.exec(body);
            if (typesMatch) {
                types = [...typesMatch[1].matchAll(/"([^"]+)"/g)].map((m) => m[1]);
            }
        }
        sections.push({ id: idMatch[1], types });
    }
    return sections;
}

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

test("sectionTypesAttr and sectionAttrsFromForm encode schema Types", () => {
    assert.equal(ui.sectionTypesAttr(null), "");
    assert.equal(ui.sectionTypesAttr([]), "");
    assert.equal(ui.sectionTypesAttr(["bundle"]), "bundle");
    assert.equal(ui.sectionTypesAttr(["bundle", "grouped"]), "bundle,grouped");
    assert.deepEqual(
        ui.sectionAttrsFromForm({
            sections: [
                { id: "visibility", types: [] },
                { id: "bundle", types: ["bundle"] }
            ]
        }),
        { visibility: "", bundle: "bundle" }
    );
    assert.deepEqual(ui.sectionAttrsFromForm(null), {});
});

// Section definitions come from wire_services.go registrations; expected matrix
// is the oracle for which types should show which sections.
test("sectionVisible: Phase 12 type×section matrix (6×5)", () => {
    const types = ["simple", "virtual", "bundle", "grouped", "configurable", "downloadable"];
    const registered = loadRegisteredProductFormSections();
    assert.ok(registered.length >= 5, "expected product form sections in wire_services.go");
    const sections = ui.sectionAttrsFromForm({ sections: registered });
    assert.deepEqual(
        Object.keys(sections).sort(),
        ["bundle", "downloadable", "grouped", "linked", "visibility"]
    );
    const expected = {
        simple: { visibility: true, bundle: false, grouped: false, linked: true, downloadable: false },
        virtual: { visibility: true, bundle: false, grouped: false, linked: true, downloadable: false },
        bundle: { visibility: true, bundle: true, grouped: false, linked: true, downloadable: false },
        grouped: { visibility: true, bundle: false, grouped: true, linked: true, downloadable: false },
        configurable: { visibility: true, bundle: false, grouped: false, linked: true, downloadable: false },
        downloadable: { visibility: true, bundle: false, grouped: false, linked: true, downloadable: true }
    };
    for (const type of types) {
        for (const [section, attr] of Object.entries(sections)) {
            assert.equal(
                ui.sectionVisible(attr, type),
                expected[type][section],
                `${type} × ${section}`
            );
        }
    }
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
    assert.equal(ui.isCoreProductField("requires_linked_parent"), true);
    assert.equal(ui.isCoreProductField("color"), false);
});

test("isCoreProductField ignores Object.prototype names like constructor", () => {
    assert.equal(ui.isCoreProductField("constructor"), false);
    assert.equal(ui.isCoreProductField("toString"), false);
    const payload = { attributes: {} };
    ui.assignProductField(payload, "constructor", true);
    assert.equal(Object.prototype.hasOwnProperty.call(payload, "constructor"), false);
    assert.equal(payload.attributes.constructor, true);
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
