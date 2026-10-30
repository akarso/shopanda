// product_linked_section.js — Linked eligibility product form section (PR-1073).
// Loaded before admin.js. CommonJS-exported for Node tests.
(function (root) {
    var SECTION_LINKED = "linked";
    var PRODUCT_PAGE_LIMIT = 100;

    function esc(s) {
        return String(s == null ? "" : s)
            .replace(/&/g, "&amp;")
            .replace(/</g, "&lt;")
            .replace(/>/g, "&gt;")
            .replace(/"/g, "&quot;");
    }

    function renderLinkedSectionHTML(isEdit, requiresLinkedParent) {
        var flagNote = "";
        if (isEdit) {
            flagNote = '<p class="settings-scope-note" data-linked-requires-flag>' +
                (requiresLinkedParent
                    ? "Requires linked parent is on — the product is not purchasable until an assignment context is present."
                    : "Requires linked parent is off. Toggle it on the product form above.") +
                "</p>";
        }
        if (!isEdit) {
            return '<p class="settings-scope-note">Save the product to define linked eligibility rules. Use the Requires linked parent checkbox on create/update to gate purchasability.</p>';
        }
        return '' +
            '<div data-linked-eligibility>' +
            flagNote +
            '<h4>Eligible parent products</h4>' +
            '<div data-linked-parents></div>' +
            '<h4>Eligible categories</h4>' +
            '<div data-linked-categories></div>' +
            '<p class="settings-scope-note">A ticket (child) may list explicit parent products and/or categories. This Linked eligibility section appears on every product edit form (type-agnostic). Assignment is instance-level and uses the Assignments page.</p>' +
            '</div>';
    }

    function parentRowHTML(row) {
        row = row || {};
        return '<tr data-parent-id="' + esc(row.parent_product_id || "") + '">' +
            '<td>' + esc(row.name || row.parent_product_id || "") + '</td>' +
            '<td>' + esc(row.slug || "") + '</td>' +
            '<td>' + esc(row.type || "") + '</td>' +
            '<td><button type="button" data-remove-parent="' + esc(row.parent_product_id || "") + '">Remove</button></td>' +
            '</tr>';
    }

    function categoryRowHTML(row) {
        row = row || {};
        return '<tr data-category-id="' + esc(row.category_id || "") + '">' +
            '<td>' + esc(row.name || row.category_id || "") + '</td>' +
            '<td>' + esc(row.slug || "") + '</td>' +
            '<td><button type="button" data-remove-category="' + esc(row.category_id || "") + '">Remove</button></td>' +
            '</tr>';
    }

    function parentsTableHTML(rows) {
        rows = rows || [];
        var html = '<table><thead><tr><th>Name</th><th>Slug</th><th>Type</th><th></th></tr></thead><tbody>';
        if (!rows.length) {
            html += '<tr><td colspan="4">No parent allowlist rows.</td></tr>';
        } else {
            for (var i = 0; i < rows.length; i++) {
                html += parentRowHTML(rows[i]);
            }
        }
        html += '</tbody></table>';
        html += '<form data-add-parent-form style="margin-top:0.5rem">' +
            '<label>Parent product id <input name="parent_product_id" required></label> ' +
            '<button type="submit">Add parent</button></form>' +
            '<p class="settings-scope-note">Paste a product id, or pick from the list below.</p>' +
            '<div data-linked-parent-picker></div>';
        return html;
    }

    function categoriesTableHTML(rows, categoryOptions) {
        rows = rows || [];
        var html = '<table><thead><tr><th>Name</th><th>Slug</th><th></th></tr></thead><tbody>';
        if (!rows.length) {
            html += '<tr><td colspan="3">No category rules.</td></tr>';
        } else {
            for (var i = 0; i < rows.length; i++) {
                html += categoryRowHTML(rows[i]);
            }
        }
        html += '</tbody></table>';
        html += '<form data-add-category-form style="margin-top:0.5rem">' +
            '<label>Category <select name="category_id"><option value="">Select…</option>';
        categoryOptions = categoryOptions || [];
        for (var j = 0; j < categoryOptions.length; j++) {
            var c = categoryOptions[j] || {};
            html += '<option value="' + esc(c.id || "") + '">' + esc(c.name || c.slug || c.id || "") + '</option>';
        }
        html += '</select></label> <button type="submit">Add category</button></form>';
        return html;
    }

    function parentPickerHTML(state) {
        state = state || {};
        var products = state.products || [];
        var html = '<div data-linked-picker-body>' +
            '<div data-linked-picker-msg></div>' +
            '<ul style="list-style:none;padding:0;max-height:12rem;overflow:auto">';
        if (!products.length) {
            html += '<li class="settings-scope-note">No products loaded.</li>';
        } else {
            for (var i = 0; i < products.length; i++) {
                var p = products[i] || {};
                if (state.excludeID && String(p.id) === String(state.excludeID)) {
                    continue;
                }
                html += '<li><button type="button" data-pick-parent-id="' + esc(p.id || "") + '">' +
                    esc(p.name || p.slug || p.id || "") +
                    (p.slug ? (' <code>' + esc(p.slug) + '</code>') : '') +
                    '</button></li>';
            }
        }
        html += '</ul>';
        if (state.hasMore) {
            html += '<button type="button" data-linked-picker-more>Load more</button>';
        }
        html += '</div>';
        return html;
    }

    // bindLinkedParentPicker pages /admin/products and fills the parent_product_id input on click.
    function bindLinkedParentPicker(root, opts) {
        opts = opts || {};
        var host = root && root.querySelector ? root.querySelector("[data-linked-parent-picker]") : null;
        var form = root && root.querySelector ? root.querySelector("[data-add-parent-form]") : null;
        if (!host || !form || typeof opts.api !== "function") {
            return;
        }
        if (host.getAttribute("data-linked-picker-bound") === "1") {
            return;
        }
        host.setAttribute("data-linked-picker-bound", "1");
        var state = { products: [], offset: 0, hasMore: false, excludeID: opts.productID || "" };

        function setMessage(text, isError) {
            var el = host.querySelector("[data-linked-picker-msg]");
            if (!el) {
                return;
            }
            el.innerHTML = text ? ('<span' + (isError ? ' role="alert"' : "") + ">" + esc(text) + "</span>") : "";
        }

        function bindClicks() {
            host.querySelectorAll("[data-pick-parent-id]").forEach(function (btn) {
                btn.addEventListener("click", function () {
                    var id = btn.getAttribute("data-pick-parent-id") || "";
                    if (form.elements.parent_product_id) {
                        form.elements.parent_product_id.value = id;
                        form.elements.parent_product_id.focus();
                    }
                });
            });
            var more = host.querySelector("[data-linked-picker-more]");
            if (more) {
                more.addEventListener("click", loadMore);
            }
        }

        function render() {
            host.innerHTML = parentPickerHTML(state);
            bindClicks();
        }

        function applyPage(body) {
            body = body || {};
            if (body.error) {
                throw new Error((body.error && body.error.message) || "Failed to load products");
            }
            var page = (body.data && body.data.products) || [];
            for (var i = 0; i < page.length; i++) {
                var raw = page[i] || {};
                state.products.push({
                    id: raw.id || raw.ID || "",
                    name: raw.name || raw.Name || "",
                    slug: raw.slug || raw.Slug || ""
                });
            }
            state.offset += page.length;
            state.hasMore = page.length >= PRODUCT_PAGE_LIMIT;
        }

        function loadMore() {
            return opts.api("/admin/products?offset=" + encodeURIComponent(String(state.offset)) +
                "&limit=" + encodeURIComponent(String(PRODUCT_PAGE_LIMIT))).then(function (body) {
                applyPage(body);
                render();
            }).catch(function (err) {
                setMessage((err && err.message) || "Failed to load products", true);
            });
        }

        host.innerHTML = '<p class="settings-scope-note">Loading products…</p>';
        loadMore();
    }

    var api = {
        SECTION_LINKED: SECTION_LINKED,
        PRODUCT_PAGE_LIMIT: PRODUCT_PAGE_LIMIT,
        renderLinkedSectionHTML: renderLinkedSectionHTML,
        parentsTableHTML: parentsTableHTML,
        categoriesTableHTML: categoriesTableHTML,
        parentRowHTML: parentRowHTML,
        categoryRowHTML: categoryRowHTML,
        parentPickerHTML: parentPickerHTML,
        bindLinkedParentPicker: bindLinkedParentPicker,
        esc: esc
    };

    root.ShopandaProductLinkedSection = api;
    if (typeof module !== "undefined" && module.exports) {
        module.exports = api;
    }
})(typeof window !== "undefined" ? window : global);
