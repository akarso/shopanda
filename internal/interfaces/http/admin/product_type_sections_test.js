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
