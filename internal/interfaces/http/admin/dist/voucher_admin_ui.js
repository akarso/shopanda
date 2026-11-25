// voucher_admin_ui.js — Admin voucher register / redeem (PR-1806).
(function (root) {
    function esc(s) {
        return String(s == null ? "" : s)
            .replace(/&/g, "&amp;")
            .replace(/</g, "&lt;")
            .replace(/>/g, "&gt;")
            .replace(/"/g, "&quot;");
    }

    function voucherRowHTML(row, canWrite) {
        row = row || {};
        var actions = "";
        actions += '<button type="button" data-download-voucher-pdf="' + esc(row.id || "") + '">PDF</button> ';
        if (canWrite) {
            if (row.status !== "redeemed" && row.status !== "void") {
                actions += '<button type="button" data-redeem-voucher="' + esc(row.id || "") + '">Redeem</button> ';
            }
            if ((row.redeemed_count || 0) > 0 && row.status !== "void") {
                actions += '<button type="button" data-unredeem-voucher="' + esc(row.id || "") + '">Unredeem</button> ';
            }
            if (row.order_id) {
                actions += '<button type="button" data-resend-order-vouchers-email="' +
                    esc(row.order_id) + '">Resend email</button> ';
            }
        }
        return '<tr data-voucher-id="' + esc(row.id || "") + '">' +
            '<td><code>' + esc(row.code || "") + '</code></td>' +
            '<td>' + esc(row.status || "") + '</td>' +
            '<td>' + esc(String(row.redeemed_count != null ? row.redeemed_count : 0)) + ' / ' +
            esc(String(row.max_redemptions != null ? row.max_redemptions : 0)) + '</td>' +
            '<td>' + esc(row.order_id || "") + '</td>' +
            '<td>' + actions + '</td>' +
            '</tr>';
    }

    function vouchersTableHTML(rows, canWrite) {
        rows = rows || [];
        var html = '<table><thead><tr>' +
            '<th>Code</th><th>Status</th><th>Redemptions</th><th>Order</th><th></th>' +
            '</tr></thead><tbody>';
        if (!rows.length) {
            html += '<tr><td colspan="5">No vouchers.</td></tr>';
        } else {
            for (var i = 0; i < rows.length; i++) {
                html += voucherRowHTML(rows[i], canWrite);
            }
        }
        html += '</tbody></table>';
        return html;
    }

    function panelHTML(title, opts) {
        opts = opts || {};
        var pack = "";
        if (opts.orderID) {
            pack = '<p><button type="button" data-download-order-vouchers-pdf="' +
                esc(opts.orderID) + '">Download pack PDF</button>';
            if (opts.canWrite) {
                pack += ' <button type="button" data-reissue-order-vouchers="' +
                    esc(opts.orderID) + '">Reissue missing codes</button>';
                pack += ' <button type="button" data-resend-order-vouchers-email="' +
                    esc(opts.orderID) + '">Resend email</button>';
            }
            pack += '</p>';
        }
        return '' +
            '<section style="margin-top:2rem" data-vouchers-panel>' +
            '<h3>' + esc(title || "Vouchers") + '</h3>' +
            pack +
            '<div data-vouchers-msg></div>' +
            '<div data-vouchers-body>Loading…</div>' +
            '</section>';
    }

    function settingsFormHTML(row) {
        row = row || {};
        return '' +
            '<form data-voucher-settings-form>' +
            '<label><input type="checkbox" name="enabled" ' + (row.enabled ? 'checked' : '') + '> Enable voucher issuance</label>' +
            '<p><label>Codes per unit <input type="number" name="codes_per_unit" min="1" max="50" value="' +
            esc(String(row.codes_per_unit != null ? row.codes_per_unit : 1)) + '"></label></p>' +
            '<p><label>Max redemptions per code <input type="number" name="max_redemptions" min="1" max="1000" value="' +
            esc(String(row.max_redemptions != null ? row.max_redemptions : 1)) + '"></label></p>' +
            '<button type="submit">Save voucher settings</button>' +
            '</form>';
    }

    var api = {
        vouchersTableHTML: vouchersTableHTML,
        voucherRowHTML: voucherRowHTML,
        panelHTML: panelHTML,
        settingsFormHTML: settingsFormHTML
    };

    root.ShopandaVoucherAdminUI = api;
    if (typeof module !== "undefined" && module.exports) {
        module.exports = api;
    }
})(typeof globalThis !== "undefined" ? globalThis : this);
