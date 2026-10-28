// assignment_admin_ui.js — Admin assignment lookup helpers (PR-1073).
// Card (parent unit) list and ticket (child unit) current + assign/revoke.
(function (root) {
    function esc(s) {
        return String(s == null ? "" : s)
            .replace(/&/g, "&amp;")
            .replace(/</g, "&lt;")
            .replace(/>/g, "&gt;")
            .replace(/"/g, "&quot;");
    }

    function pageHTML() {
        return '' +
            '<h2>Assignments</h2>' +
            '<p class="settings-scope-note">Unit ids are <code>orderID/variantID</code>. Use a card (parent) unit to list assigned tickets, or a ticket (child) unit to see its current card. Status filter and Load more apply to the card list.</p>' +
            '<div id="assignment-admin-msg"></div>' +
            '<section>' +
            '<h3>Card (parent unit)</h3>' +
            '<form id="assignment-parent-form">' +
            '<label>Parent unit id <input name="parent_unit_id" required placeholder="order-uuid/variant-uuid" style="min-width:28rem"></label> ' +
            '<label>Status <select name="status">' +
            '<option value="assigned" selected>assigned</option>' +
            '<option value="revoked">revoked</option>' +
            '<option value="all">all</option>' +
            '</select></label> ' +
            '<button type="submit">List</button>' +
            '</form>' +
            '<div id="assignment-parent-results"></div>' +
            '</section>' +
            '<section style="margin-top:1.5rem">' +
            '<h3>Ticket (child unit)</h3>' +
            '<form id="assignment-child-form">' +
            '<label>Child unit id <input name="child_unit_id" required placeholder="order-uuid/variant-uuid" style="min-width:28rem"></label> ' +
            '<button type="submit">Show current card</button>' +
            '</form>' +
            '<div id="assignment-child-results"></div>' +
            '</section>' +
            '<section style="margin-top:1.5rem">' +
            '<h3>Assign</h3>' +
            '<form id="assignment-assign-form">' +
            '<label>Child unit id <input name="child_unit_id" required></label> ' +
            '<label>Parent unit id <input name="parent_unit_id" required></label> ' +
            '<label><input type="checkbox" name="force"> Force</label> ' +
            '<label>Reason <input name="reason" maxlength="500"></label> ' +
            '<button type="submit">Assign</button>' +
            '</form>' +
            '</section>';
    }

    function assignmentTableHTML(rows, opts) {
        opts = opts || {};
        rows = rows || [];
        var html = '<table><thead><tr>' +
            '<th>ID</th><th>Child unit</th><th>Parent unit</th><th>Status</th><th>Forced</th><th>Child order</th><th>Parent order</th>';
        if (opts.canWrite) {
            html += '<th></th>';
        }
        html += '</tr></thead><tbody>';
        if (!rows.length) {
            html += '<tr><td colspan="' + (opts.canWrite ? 8 : 7) + '">No assignments.</td></tr>';
        } else {
            for (var i = 0; i < rows.length; i++) {
                var r = rows[i] || {};
                html += '<tr>' +
                    '<td>' + esc(r.id || "") + '</td>' +
                    '<td><code>' + esc(r.child_order_item_id || "") + '</code></td>' +
                    '<td><code>' + esc(r.parent_order_item_id || "") + '</code></td>' +
                    '<td>' + esc(r.status || "") + '</td>' +
                    '<td>' + (r.forced ? ("yes" + (r.reason ? " — " + esc(r.reason) : "")) : "no") + '</td>' +
                    '<td>' + esc(r.child_order_status || "") + ' / ' + esc(r.child_payment_status || "") + '</td>' +
                    '<td>' + esc(r.parent_order_status || "") + ' / ' + esc(r.parent_payment_status || "") + '</td>';
                if (opts.canWrite && (r.status === "assigned" || r.status === "unassigned")) {
                    html += '<td><button type="button" data-revoke-id="' + esc(r.id || "") + '">Revoke</button></td>';
                } else if (opts.canWrite) {
                    html += '<td></td>';
                }
                html += '</tr>';
            }
        }
        html += '</tbody></table>';
        if (opts.hasMore) {
            html += '<p><button type="button" data-assignment-load-more data-offset="' +
                esc(String(opts.nextOffset != null ? opts.nextOffset : 0)) +
                '">Load more</button></p>';
        }
        return html;
    }

    function currentHTML(assignment, opts) {
        opts = opts || {};
        if (!assignment) {
            return '<p>No current assignment for this child unit.</p>';
        }
        return assignmentTableHTML([assignment], opts);
    }

    var api = {
        pageHTML: pageHTML,
        assignmentTableHTML: assignmentTableHTML,
        currentHTML: currentHTML,
        esc: esc
    };
    root.ShopandaAssignmentAdminUI = api;
    if (typeof module !== "undefined" && module.exports) {
        module.exports = api;
    }
})(typeof window !== "undefined" ? window : global);
