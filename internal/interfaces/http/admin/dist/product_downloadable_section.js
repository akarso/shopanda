// product_downloadable_section.js — Downloadable product form section (PR-1078).
// Loaded before admin.js. CommonJS-exported for Node tests.
// File attach/replace/remove use admin routes; grant settings are product-level overrides.
(function (root) {
    function esc(s) {
        return String(s == null ? "" : s)
            .replace(/&/g, "&amp;")
            .replace(/</g, "&lt;")
            .replace(/>/g, "&gt;")
            .replace(/"/g, "&quot;");
    }

    function renderDownloadableSectionHTML(isEdit) {
        if (!isEdit) {
            return '<p class="settings-scope-note">Save the product as downloadable to attach private files and override grant defaults. Uploads for grant targets must use Private download (private/downloads/).</p>';
        }
        return '' +
            '<div data-downloadable-section>' +
            '<h4>Attached files</h4>' +
            '<div data-downloadable-files></div>' +
            '<h4>Grant defaults (product override)</h4>' +
            '<div data-downloadable-settings></div>' +
            '<p class="settings-scope-note">Empty expiry/limit fields mean the global catalog.downloadable defaults apply. Attach only assets under private/downloads/ — uploads from this section always set private_download=1. Replace retargets download grants by asset id (not product-scoped). Without global_retarget, a shared asset (other products still attached) returns 422; with the flag every grant on the blob moves.</p>' +
            '</div>';
    }

    function fileRowHTML(row) {
        row = row || {};
        var position = row.position != null ? row.position : 0;
        var label = row.label || "";
        return '<tr data-asset-id="' + esc(row.asset_id || "") + '"' +
            ' data-position="' + esc(String(position)) + '"' +
            ' data-label="' + esc(label) + '">' +
            '<td>' + esc(label || row.filename || row.asset_id || "") + '</td>' +
            '<td>' + esc(row.filename || "") + '</td>' +
            '<td>' + esc(String(position)) + '</td>' +
            '<td><code>' + esc(row.asset_id || "") + '</code></td>' +
            '<td>' +
            '<button type="button" data-replace-file="' + esc(row.asset_id || "") + '">Replace</button> ' +
            '<button type="button" data-remove-file="' + esc(row.asset_id || "") + '">Remove</button>' +
            '</td>' +
            '</tr>';
    }

    function filesTableHTML(rows) {
        rows = rows || [];
        var html = '<table><thead><tr><th>Label</th><th>Filename</th><th>Position</th><th>Asset id</th><th></th></tr></thead><tbody>';
        if (!rows.length) {
            html += '<tr><td colspan="5">No files attached. A downloadable product needs at least one file before it can be Active.</td></tr>';
        } else {
            for (var i = 0; i < rows.length; i++) {
                html += fileRowHTML(rows[i]);
            }
        }
        html += '</tbody></table>';
        html += '<form data-attach-file-form style="margin-top:0.5rem">' +
            '<label>Asset id <input name="asset_id" required></label> ' +
            '<label>Position <input name="position" type="number" min="0" value="0"></label> ' +
            '<label>Label <input name="label"></label> ' +
            '<button type="submit">Attach</button></form>' +
            '<form data-private-upload-form style="margin-top:0.75rem">' +
            '<label>Upload private file <input type="file" name="file" required></label> ' +
            '<button type="submit">Upload &amp; attach</button></form>' +
            '<p class="settings-scope-note">Upload always sets private_download=1. Or paste an existing private asset id above.</p>';
        return html;
    }

    function settingsFormHTML(settings) {
        settings = settings || {};
        var expiry = settings.expiry_days != null ? String(settings.expiry_days) : "";
        var limit = settings.download_limit != null ? String(settings.download_limit) : "";
        return '' +
            '<form data-grant-settings-form>' +
            '<label>Expiry days <input name="expiry_days" type="number" min="1" max="3650" value="' + esc(expiry) + '" placeholder="global default"></label> ' +
            '<label>Download limit <input name="download_limit" type="number" min="1" max="10000" value="' + esc(limit) + '" placeholder="global default"></label> ' +
            '<button type="submit">Save overrides</button> ' +
            '<button type="button" data-clear-grant-settings>Clear overrides</button>' +
            '</form>';
    }

    var api = {
        renderDownloadableSectionHTML: renderDownloadableSectionHTML,
        filesTableHTML: filesTableHTML,
        settingsFormHTML: settingsFormHTML,
        fileRowHTML: fileRowHTML
    };

    root.ShopandaProductDownloadableSection = api;
    if (typeof module !== "undefined" && module.exports) {
        module.exports = api;
    }
})(typeof globalThis !== "undefined" ? globalThis : this);
