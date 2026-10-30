const { test } = require("node:test");
const assert = require("node:assert/strict");
const ui = require("./dist/product_linked_section.js");

test("renderLinkedSectionHTML create vs edit", () => {
    assert.match(ui.renderLinkedSectionHTML(false), /Save the product/);
    assert.match(ui.renderLinkedSectionHTML(true), /data-linked-eligibility/);
    assert.match(ui.renderLinkedSectionHTML(true, true), /Requires linked parent is on/);
    assert.match(ui.renderLinkedSectionHTML(true, false), /Requires linked parent is off/);
});

test("parentsTableHTML includes add form and picker", () => {
    const html = ui.parentsTableHTML([{ parent_product_id: "p1", name: "Card", slug: "card", type: "simple" }]);
    assert.match(html, /data-add-parent-form/);
    assert.match(html, /data-linked-parent-picker/);
    assert.match(html, /Card/);
});

test("esc escapes hostile names", () => {
    const html = ui.parentRowHTML({
        parent_product_id: "p1",
        name: '<img src=x onerror=alert(1)>',
        slug: 'a"b',
        type: "simple"
    });
    assert.match(html, /&lt;img/);
    assert.doesNotMatch(html, /<img src/);
    assert.match(html, /a&quot;b/);
});

test("parentPickerHTML has load more when hasMore", () => {
    const html = ui.parentPickerHTML({
        products: [{ id: "p1", name: "Card", slug: "card" }],
        hasMore: true
    });
    assert.match(html, /data-pick-parent-id="p1"/);
    assert.match(html, /data-linked-picker-more/);
});
