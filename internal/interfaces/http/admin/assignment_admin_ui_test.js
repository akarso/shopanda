const { test } = require("node:test");
const assert = require("node:assert/strict");
const ui = require("./dist/assignment_admin_ui.js");

test("pageHTML has parent and child forms with status filter", () => {
    const html = ui.pageHTML();
    assert.match(html, /assignment-parent-form/);
    assert.match(html, /assignment-child-form/);
    assert.match(html, /assignment-assign-form/);
    assert.match(html, /name="status"/);
    assert.match(html, /option value="revoked"/);
});

test("assignmentTableHTML revoke button for assigned", () => {
    const html = ui.assignmentTableHTML([{
        id: "a1",
        child_order_item_id: "o1/v1",
        parent_order_item_id: "o2/v2",
        status: "assigned"
    }], { canWrite: true });
    assert.match(html, /data-revoke-id="a1"/);
});

test("assignmentTableHTML load more when hasMore", () => {
    const html = ui.assignmentTableHTML([], { hasMore: true, nextOffset: 100 });
    assert.match(html, /data-assignment-load-more/);
    assert.match(html, /data-offset="100"/);
});

test("assignmentTableHTML escapes hostile reason", () => {
    const html = ui.assignmentTableHTML([{
        id: "a1",
        child_order_item_id: "o1/v1",
        parent_order_item_id: "o2/v2",
        status: "assigned",
        forced: true,
        reason: '<script>alert(1)</script>'
    }], { canWrite: false });
    assert.match(html, /&lt;script&gt;/);
    assert.doesNotMatch(html, /<script>alert/);
});
