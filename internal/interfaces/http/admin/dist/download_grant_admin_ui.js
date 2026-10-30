// download_grant_admin_ui.js — Admin download grant list / revoke / re-issue (PR-1078).
// Mounted on order detail and customer detail. CommonJS-exported for Node tests.
(function (root) {
    function esc(s) {
        return String(s == null ? "" : s)
            .replace(/&/g, "&amp;")
            .replace(/</g, "&lt;")
            .replace(/>/g, "&gt;")
            .replace(/"/g, "&quot;");
    }

    function grantRowHTML(row, canWrite) {
        row = row || {};
        var status = [];
        if (row.expired) { status.push("expired"); }
        if (row.exhausted) { status.push("exhausted"); }
        if (!status.length) { status.push("live"); }
        var actions = "";
        if (canWrite) {
            actions =
                '<button type="button" data-revoke-grant="' + esc(row.id || "") + '">Revoke</button> ' +
                '<button type="button" data-reissue-grant="' + esc(row.id || "") + '">Re-issue</button>';
        }
        return '<tr data-grant-id="' + esc(row.id || "") + '">' +
            '<td><code>' + esc(row.id || "") + '</code></td>' +
            '<td>' + esc(row.order_item_id || "") + '</td>' +
            '<td>' + esc(row.asset_id || "") + '</td>' +
            '<td>' + esc(String(row.download_count != null ? row.download_count : 0)) + ' / ' +
            esc(String(row.download_limit != null ? row.download_limit : 0)) + '</td>' +
            '<td>' + esc(row.expires_at || "") + '</td>' +
            '<td>' + esc(status.join(", ")) + '</td>' +
            '<td>' + actions + '</td>' +
            '</tr>';
    }

    function grantsTableHTML(rows, canWrite) {
        rows = rows || [];
        var html = '<table><thead><tr>' +
            '<th>Grant id</th><th>Order item</th><th>Asset</th><th>Count</th><th>Expires</th><th>Status</th><th></th>' +
            '</tr></thead><tbody>';
        if (!rows.length) {
            html += '<tr><td colspan="7">No download grants.</td></tr>';
        } else {
            for (var i = 0; i < rows.length; i++) {
                html += grantRowHTML(rows[i], canWrite);
            }
        }
        html += '</tbody></table>';
        return html;
    }

    function panelHTML(title) {
        return '' +
            '<section style="margin-top:2rem" data-download-grants-panel>' +
            '<h3>' + esc(title || "Download grants") + '</h3>' +
            '<div data-download-grants-msg></div>' +
            '<div data-download-grants-body>Loading…</div>' +
            '</section>';
    }

    var api = {
        grantsTableHTML: grantsTableHTML,
        grantRowHTML: grantRowHTML,
        panelHTML: panelHTML
    };

    root.ShopandaDownloadGrantAdminUI = api;
    if (typeof module !== "undefined" && module.exports) {
        module.exports = api;
    }
})(typeof globalThis !== "undefined" ? globalThis : this);
