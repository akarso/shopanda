// product_composition_section.js — Bundle and grouped product form sections (PR-1066).
// Loaded before admin.js. CommonJS-exported for Node tests.
// Bundle mode fields are core product fields (pricing_mode, stock_mode,
// explode_order_lines) collected by collectProductPayload when type is bundle.
// Component and member links use their own admin routes and are edit-only.
(function (root) {
    var LEAF_TYPES = ["simple", "virtual", "downloadable"];
    var LEAF_PAGE_LIMIT = 100;
    // Caps loadLeafType when it walks pages itself and every page is full.
    // Load more calls loadLeafProducts once per click and stops only on a short page.
    var LEAF_MAX_PAGES = 50;
    var SECTION_BUNDLE = "bundle";
    var SECTION_GROUPED = "grouped";
    var MODE_OPTIONS = ["fixed", "dynamic"];

    function esc(s) {
        return String(s == null ? "" : s)
            .replace(/&/g, "&amp;")
            .replace(/</g, "&lt;")
            .replace(/>/g, "&gt;")
            .replace(/"/g, "&quot;");
    }

    function field(raw, snake, pascal) {
        if (!raw) {
            return undefined;
        }
        if (raw[snake] != null) {
            return raw[snake];
        }
        if (raw[pascal] != null) {
            return raw[pascal];
        }
        return undefined;
    }

    function modesFromProduct(product) {
        var pricing = field(product, "pricingMode", "pricing_mode");
        if (pricing == null) {
            pricing = field(product, "PricingMode", "PricingMode");
        }
        if (product && pricing == null && product.pricing_mode == null && product.PricingMode != null) {
            pricing = product.PricingMode;
        }
        var stock = product && (product.stockMode != null ? product.stockMode : (product.stock_mode != null ? product.stock_mode : product.StockMode));
        var explode = false;
        if (product) {
            if (product.explodeOrderLines != null) {
                explode = !!product.explodeOrderLines;
            } else if (product.explode_order_lines != null) {
                explode = !!product.explode_order_lines;
            } else if (product.ExplodeOrderLines != null) {
                explode = !!product.ExplodeOrderLines;
            }
        }
        return {
            pricing_mode: String(pricing || "fixed"),
            stock_mode: String(stock || "fixed"),
            explode_order_lines: !!explode
        };
    }

    function explodeEnabled(pricing, stock) {
        return pricing === "dynamic" && stock === "dynamic";
    }

    function selectOptions(selected) {
        var html = "";
        for (var i = 0; i < MODE_OPTIONS.length; i++) {
            var value = MODE_OPTIONS[i];
            html += '<option value="' + value + '"' + (value === selected ? " selected" : "") + ">" + value + "</option>";
        }
        return html;
    }

    function renderBundleModesHTML(modes) {
        modes = modes || modesFromProduct(null);
        var enabled = explodeEnabled(modes.pricing_mode, modes.stock_mode);
        var checked = enabled && modes.explode_order_lines ? " checked" : "";
        var disabled = enabled ? "" : " disabled";
        return '' +
            '<div class="product-bundle-modes">' +
            '<label>Pricing mode<select id="product-bundle-pricing-mode">' + selectOptions(modes.pricing_mode) + "</select></label>" +
            '<label>Stock mode<select id="product-bundle-stock-mode">' + selectOptions(modes.stock_mode) + "</select></label>" +
            '<label><input type="checkbox" id="product-bundle-explode-order-lines"' + checked + disabled + "> Explode order lines</label>" +
            '<p class="settings-scope-note">Explode order lines is available only when pricing and stock are both dynamic. These fields save with the product.</p>' +
            "</div>";
    }

    function renderBundleSectionHTML(modes, isEdit) {
        var html = renderBundleModesHTML(modes);
        if (!isEdit) {
            html += '<p class="settings-scope-note">Save the product to add bundle components.</p>';
            return html;
        }
        html += '<div data-bundle-picker></div>';
        return html;
    }

    function renderGroupedSectionHTML(isEdit) {
        if (!isEdit) {
            return '<p class="settings-scope-note">Save the product to add grouped members.</p>';
        }
        return '<div data-grouped-picker></div>';
    }

    function collectModes(root, productType) {
        if (String(productType || "") !== "bundle") {
            return null;
        }
        if (!root || typeof root.querySelector !== "function") {
            return null;
        }
        var pricing = root.querySelector("#product-bundle-pricing-mode");
        var stock = root.querySelector("#product-bundle-stock-mode");
        if (!pricing || !stock) {
            return null;
        }
        var pricingVal = pricing.value || "fixed";
        var stockVal = stock.value || "fixed";
        var explodeEl = root.querySelector("#product-bundle-explode-order-lines");
        var explode = false;
        if (explodeEl && explodeEnabled(pricingVal, stockVal)) {
            explode = !!explodeEl.checked;
        }
        return {
            pricing_mode: pricingVal,
            stock_mode: stockVal,
            explode_order_lines: explode
        };
    }

    function syncExplodeControl(pricingEl, stockEl, explodeEl) {
        if (!pricingEl || !stockEl || !explodeEl) {
            return;
        }
        var enabled = explodeEnabled(pricingEl.value, stockEl.value);
        explodeEl.disabled = !enabled;
        if (!enabled) {
            explodeEl.checked = false;
        }
    }

    function bindBundleModes(root) {
        if (!root || typeof root.querySelector !== "function") {
            return;
        }
        var pricing = root.querySelector("#product-bundle-pricing-mode");
        var stock = root.querySelector("#product-bundle-stock-mode");
        var explode = root.querySelector("#product-bundle-explode-order-lines");
        function refresh() {
            syncExplodeControl(pricing, stock, explode);
        }
        if (pricing && pricing.addEventListener) {
            pricing.addEventListener("change", refresh);
        }
        if (stock && stock.addEventListener) {
            stock.addEventListener("change", refresh);
        }
        refresh();
    }

    function normalizeListedProduct(raw) {
        if (!raw) {
            return null;
        }
        var id = raw.id || raw.ID || "";
        if (!id) {
            return null;
        }
        return {
            id: String(id),
            name: String(raw.name || raw.Name || ""),
            slug: String(raw.slug || raw.Slug || ""),
            type: String(raw.type || raw.Type || "")
        };
    }

    function mergeLeafProducts(byType, parentID) {
        var parent = String(parentID || "");
        var out = [];
        var seen = {};
        byType = byType || {};
        for (var t = 0; t < LEAF_TYPES.length; t++) {
            var list = byType[LEAF_TYPES[t]] || [];
            for (var i = 0; i < list.length; i++) {
                var product = normalizeListedProduct(list[i]);
                if (!product || product.id === parent || seen[product.id]) {
                    continue;
                }
                if (product.type && LEAF_TYPES.indexOf(product.type) === -1) {
                    continue;
                }
                if (!product.type) {
                    product.type = LEAF_TYPES[t];
                }
                seen[product.id] = true;
                out.push(product);
            }
        }
        return out;
    }

    // filterBySearch matches products already loaded into the picker.
    // It does not query the catalog for names outside those pages.
    function filterBySearch(products, search) {
        var query = String(search || "").trim().toLowerCase();
        if (!query) {
            return (products || []).slice();
        }
        var out = [];
        for (var i = 0; i < products.length; i++) {
            var p = products[i];
            var text = ((p.name || "") + " " + (p.slug || "") + " " + (p.id || "")).toLowerCase();
            if (text.indexOf(query) !== -1) {
                out.push(p);
            }
        }
        return out;
    }

    function bundleAssignmentRequest(parentID, componentID, checked, quantity, required) {
        if (checked) {
            var qty = parseInt(quantity, 10);
            if (!qty || qty < 1) {
                qty = 1;
            }
            return {
                method: "POST",
                path: "/admin/products/" + encodeURIComponent(parentID) + "/bundle-components",
                body: {
                    component_product_id: componentID,
                    quantity: qty,
                    required: !!required
                }
            };
        }
        return {
            method: "DELETE",
            path: "/admin/products/" + encodeURIComponent(parentID) + "/bundle-components/" + encodeURIComponent(componentID),
            body: null
        };
    }

    function groupedAssignmentRequest(parentID, memberID, checked) {
        if (checked) {
            return {
                method: "POST",
                path: "/admin/products/" + encodeURIComponent(parentID) + "/grouped-members",
                body: { member_product_id: memberID }
            };
        }
        return {
            method: "DELETE",
            path: "/admin/products/" + encodeURIComponent(parentID) + "/grouped-members/" + encodeURIComponent(memberID),
            body: null
        };
    }

    function renderBundleListHTML(model) {
        model = model || {};
        var products = filterBySearch(model.products || [], model.search);
        var assigned = model.assigned || {};
        var canWrite = !!model.canWrite;
        if (!products.length) {
            return '<p class="settings-scope-note">No simple, virtual, or downloadable products match.</p>';
        }
        var html = '<ul class="admin-tree-list">';
        for (var i = 0; i < products.length; i++) {
            var p = products[i];
            var link = assigned[p.id];
            var checked = link ? " checked" : "";
            var disabled = canWrite ? "" : " disabled";
            var qty = link ? link.quantity : 1;
            var required = !link || link.required !== false;
            html += '<li><label><input type="checkbox" data-bundle-toggle="' + esc(p.id) + '"' + checked + disabled + "> " +
                "<strong>" + esc(p.name || p.slug || p.id) + "</strong> <span class=\"settings-scope-note\">/" + esc(p.slug || p.id) + " · " + esc(p.type) + "</span></label> " +
                '<label>Qty <input type="number" min="1" max="9999" data-bundle-qty="' + esc(p.id) + '" value="' + esc(String(qty)) + '"' + disabled + "></label> " +
                '<label><input type="checkbox" data-bundle-required="' + esc(p.id) + '"' + (required ? " checked" : "") + disabled + "> Required</label></li>";
        }
        html += "</ul>";
        return html;
    }

    function renderBundlePickerBody(model) {
        model = model || {};
        var canWrite = !!model.canWrite;
        var html = '<div class="product-bundle-picker">';
        html += '<label>Filter products<input type="search" data-bundle-search placeholder="Search loaded products" value="' + esc(model.search || "") + '"></label>';
        html += '<p class="settings-scope-note">Search filters loaded products only. Load more fetches the next page.</p>';
        if (!canWrite) {
            html += '<p class="settings-scope-note">Your account can view components but cannot change them without products write access.</p>';
        }
        html += '<p data-bundle-msg></p>';
        html += '<div data-bundle-list>' + renderBundleListHTML(model) + "</div>";
        html += '<div data-bundle-more>' + (model.hasMore ? '<button type="button" data-bundle-load-more>Load more</button>' : "") + "</div></div>";
        return html;
    }

    function renderGroupedListHTML(model) {
        model = model || {};
        var products = filterBySearch(model.products || [], model.search);
        var assigned = model.assigned || {};
        var canWrite = !!model.canWrite;
        if (!products.length) {
            return '<p class="settings-scope-note">No simple, virtual, or downloadable products match.</p>';
        }
        var html = '<ul class="admin-tree-list">';
        for (var i = 0; i < products.length; i++) {
            var p = products[i];
            var checked = assigned[p.id] ? " checked" : "";
            var disabled = canWrite ? "" : " disabled";
            html += '<li><label><input type="checkbox" data-grouped-toggle="' + esc(p.id) + '"' + checked + disabled + "> " +
                "<strong>" + esc(p.name || p.slug || p.id) + "</strong> <span class=\"settings-scope-note\">/" + esc(p.slug || p.id) + " · " + esc(p.type) + "</span></label></li>";
        }
        html += "</ul>";
        return html;
    }

    function renderGroupedPickerBody(model) {
        model = model || {};
        var canWrite = !!model.canWrite;
        var html = '<div class="product-grouped-picker">';
        html += '<label>Filter products<input type="search" data-grouped-search placeholder="Search loaded products" value="' + esc(model.search || "") + '"></label>';
        html += '<p class="settings-scope-note">Search filters loaded products only. Load more fetches the next page.</p>';
        if (!canWrite) {
            html += '<p class="settings-scope-note">Your account can view members but cannot change them without products write access.</p>';
        }
        html += '<p data-grouped-msg></p>';
        html += '<div data-grouped-list>' + renderGroupedListHTML(model) + "</div>";
        html += '<div data-grouped-more>' + (model.hasMore ? '<button type="button" data-grouped-load-more>Load more</button>' : "") + "</div></div>";
        return html;
    }

    function productsFromListBody(body) {
        var raw = body && body.data && body.data.products;
        return Array.isArray(raw) ? raw : [];
    }

    function createLeafCache() {
        return {
            byType: { simple: [], virtual: [], downloadable: [] },
            offset: { simple: 0, virtual: 0, downloadable: 0 },
            pages: { simple: 0, virtual: 0, downloadable: 0 },
            done: { simple: false, virtual: false, downloadable: false }
        };
    }

    function leafCacheHasMore(cache) {
        if (!cache || !cache.done) {
            return false;
        }
        for (var i = 0; i < LEAF_TYPES.length; i++) {
            if (!cache.done[LEAF_TYPES[i]]) {
                return true;
            }
        }
        return false;
    }

    // loadLeafType appends one or more pages for a single type.
    // pagesLeft defaults to LEAF_MAX_PAGES so a full page every time cannot recurse forever.
    // Pass 1 to fetch only the page at offset.
    function loadLeafType(api, type, offset, acc, pagesLeft) {
        if (pagesLeft == null) {
            pagesLeft = LEAF_MAX_PAGES;
        }
        if (pagesLeft <= 0) {
            return Promise.resolve(acc);
        }
        var url = "/admin/products?type=" + encodeURIComponent(type) +
            "&offset=" + encodeURIComponent(String(offset)) +
            "&limit=" + encodeURIComponent(String(LEAF_PAGE_LIMIT));
        return api(url).then(function (body) {
            body = body || {};
            if (body.error) {
                var err = new Error(body.error.message || "Failed to load products");
                err.body = body;
                throw err;
            }
            var page = productsFromListBody(body);
            for (var i = 0; i < page.length; i++) {
                acc.push(page[i]);
            }
            if (page.length < LEAF_PAGE_LIMIT) {
                return acc;
            }
            return loadLeafType(api, type, offset + LEAF_PAGE_LIMIT, acc, pagesLeft - 1);
        });
    }

    // loadLeafProducts fetches the next page of each leaf type that is not finished
    // and returns every product loaded into cache so far. The first call is the
    // first page (limit 100 per type). A later call with the same cache is Load more.
    function loadLeafProducts(api, parentID, cache) {
        if (!cache) {
            cache = createLeafCache();
        }
        return Promise.all(LEAF_TYPES.map(function (type) {
            if (cache.done[type]) {
                return Promise.resolve(null);
            }
            var offset = cache.offset[type];
            return loadLeafType(api, type, offset, [], 1).then(function (page) {
                return { type: type, page: page || [], offset: offset };
            });
        })).then(function (updates) {
            for (var u = 0; u < updates.length; u++) {
                var update = updates[u];
                if (!update) {
                    continue;
                }
                var bucket = cache.byType[update.type] || [];
                for (var i = 0; i < update.page.length; i++) {
                    bucket.push(update.page[i]);
                }
                cache.byType[update.type] = bucket;
                cache.pages[update.type] += 1;
                cache.offset[update.type] = update.offset + update.page.length;
                if (update.page.length < LEAF_PAGE_LIMIT) {
                    cache.done[update.type] = true;
                }
            }
            return mergeLeafProducts(cache.byType, parentID);
        });
    }

    function productsWithAssigned(loaded, parentID, rows, readID) {
        var products = (loaded || []).slice();
        var known = {};
        var assigned = [];
        for (var i = 0; i < products.length; i++) {
            known[products[i].id] = true;
        }
        rows = rows || [];
        for (var j = 0; j < rows.length; j++) {
            var row = rows[j] || {};
            var id = String(readID(row) || "");
            if (!id || id === String(parentID || "")) {
                continue;
            }
            assigned.push({ id: id, row: row });
            if (!known[id]) {
                products.push({
                    id: id,
                    name: row.name || id,
                    slug: row.slug || "",
                    type: row.type || ""
                });
                known[id] = true;
            }
        }
        return { products: products, assigned: assigned };
    }

    function openLeafSession(api, parentID) {
        var cache = createLeafCache();
        var ready = false;
        function ensure() {
            if (ready) {
                return Promise.resolve(mergeLeafProducts(cache.byType, parentID));
            }
            return loadLeafProducts(api, parentID, cache).then(function (products) {
                ready = true;
                return products;
            });
        }
        function more() {
            return loadLeafProducts(api, parentID, cache).then(function (products) {
                ready = true;
                return products;
            });
        }
        return {
            ensure: ensure,
            more: more,
            hasMore: function () { return leafCacheHasMore(cache); }
        };
    }

    function bindBundlePicker(root, opts) {
        opts = opts || {};
        var host = root && root.querySelector ? root.querySelector("[data-bundle-picker]") : null;
        if (!host || !opts.productID || typeof opts.api !== "function") {
            return;
        }
        if (host.getAttribute("data-composition-picker-bound") === "1") {
            return;
        }
        host.setAttribute("data-composition-picker-bound", "1");
        var leaves = openLeafSession(opts.api, opts.productID);
        var componentRows = [];
        var state = { products: [], assigned: {}, search: "", canWrite: !!opts.canWrite, hasMore: false };
        function setMessage(text, isError) {
            var el = host.querySelector("[data-bundle-msg]");
            if (!el) {
                return;
            }
            el.innerHTML = text ? '<span' + (isError ? ' role="alert"' : "") + ">" + esc(text) + "</span>" : "";
        }
        function bindListControls() {
            var toggles = host.querySelectorAll("[data-bundle-toggle]");
            for (var i = 0; i < toggles.length; i++) {
                toggles[i].addEventListener("change", onToggle);
            }
            var qtys = host.querySelectorAll("[data-bundle-qty]");
            for (var q = 0; q < qtys.length; q++) {
                qtys[q].addEventListener("change", onQty);
            }
            var reqs = host.querySelectorAll("[data-bundle-required]");
            for (var r = 0; r < reqs.length; r++) {
                reqs[r].addEventListener("change", onRequired);
            }
        }
        function renderList() {
            var list = host.querySelector("[data-bundle-list]");
            if (!list) {
                return;
            }
            list.innerHTML = renderBundleListHTML(state);
            bindListControls();
        }
        function render() {
            host.innerHTML = renderBundlePickerBody(state);
            var search = host.querySelector("[data-bundle-search]");
            if (search) {
                search.addEventListener("input", function () {
                    state.search = search.value || "";
                    renderList();
                });
            }
            bindListControls();
            bindLoadMore();
        }
        function bindLoadMore() {
            var btn = host.querySelector("[data-bundle-load-more]");
            if (!btn) {
                return;
            }
            btn.addEventListener("click", onLoadMore);
        }
        function syncComponentRow(id, quantity, required) {
            var key = String(id);
            var found = false;
            for (var i = 0; i < componentRows.length; i++) {
                var row = componentRows[i];
                if (row && String(row.component_product_id) === key) {
                    row.quantity = quantity;
                    row.required = required;
                    found = true;
                }
            }
            if (!found) {
                componentRows.push({
                    component_product_id: key,
                    quantity: quantity,
                    required: required
                });
            }
        }
        function applyLoaded(loaded) {
            var merged = productsWithAssigned(loaded, opts.productID, componentRows, function (row) {
                return row.component_product_id;
            });
            state.products = merged.products;
            state.assigned = {};
            for (var i = 0; i < merged.assigned.length; i++) {
                var item = merged.assigned[i];
                var row = item.row || {};
                state.assigned[item.id] = { quantity: row.quantity || 1, required: row.required !== false };
            }
            state.hasMore = leaves.hasMore();
        }
        function onLoadMore() {
            leaves.more().then(function (loaded) {
                applyLoaded(loaded);
                renderList();
                var slot = host.querySelector("[data-bundle-more]");
                if (slot) {
                    slot.innerHTML = state.hasMore ? '<button type="button" data-bundle-load-more>Load more</button>' : "";
                }
                bindLoadMore();
            }).catch(function (err) {
                setMessage((err && err.message) || "Failed to load products", true);
            });
        }
        function showFailure(body) {
            var message = (body && body.error && body.error.message) || "Save failed";
            return reload().then(function () {
                setMessage(message, true);
            });
        }
        function send(req) {
            var options = { method: req.method };
            if (req.body) {
                options.body = JSON.stringify(req.body);
            }
            return opts.api(req.path, options);
        }
        function onToggle(e) {
            if (!state.canWrite) {
                e.target.checked = !e.target.checked;
                return;
            }
            var id = e.target.getAttribute("data-bundle-toggle");
            var qtyEl = host.querySelector('[data-bundle-qty="' + id + '"]');
            var reqEl = host.querySelector('[data-bundle-required="' + id + '"]');
            var req = bundleAssignmentRequest(opts.productID, id, e.target.checked, qtyEl ? qtyEl.value : 1, reqEl ? reqEl.checked : true);
            send(req).then(function (body) {
                if (body && body.error) {
                    return showFailure(body);
                }
                return reload();
            }).catch(function () {
                return showFailure(null);
            });
        }
        function onQty(e) {
            var id = e.target.getAttribute("data-bundle-qty");
            if (!state.assigned[id] || !state.canWrite) {
                return;
            }
            var reqEl = host.querySelector('[data-bundle-required="' + id + '"]');
            var req = bundleAssignmentRequest(opts.productID, id, true, e.target.value, reqEl ? reqEl.checked : true);
            send(req).then(function (body) {
                if (body && body.error) {
                    return showFailure(body);
                }
                state.assigned[id].quantity = req.body.quantity;
                state.assigned[id].required = req.body.required;
                syncComponentRow(id, req.body.quantity, req.body.required);
                setMessage("Component saved", false);
            }).catch(function () {
                return showFailure(null);
            });
        }
        function onRequired(e) {
            var id = e.target.getAttribute("data-bundle-required");
            if (!state.assigned[id] || !state.canWrite) {
                e.target.checked = !e.target.checked;
                return;
            }
            var qtyEl = host.querySelector('[data-bundle-qty="' + id + '"]');
            var req = bundleAssignmentRequest(opts.productID, id, true, qtyEl ? qtyEl.value : 1, e.target.checked);
            send(req).then(function (body) {
                if (body && body.error) {
                    return showFailure(body);
                }
                state.assigned[id].quantity = req.body.quantity;
                state.assigned[id].required = req.body.required;
                syncComponentRow(id, req.body.quantity, req.body.required);
                setMessage("Component saved", false);
            }).catch(function () {
                return showFailure(null);
            });
        }
        function reload() {
            host.innerHTML = "<p>Loading…</p>";
            return Promise.all([
                opts.api("/admin/products/" + encodeURIComponent(opts.productID) + "/bundle-components"),
                leaves.ensure()
            ]).then(function (results) {
                var listBody = results[0] || {};
                if (listBody.error) {
                    host.innerHTML = '<p role="alert">' + esc(listBody.error.message || "Failed to load components") + "</p>";
                    return;
                }
                componentRows = (listBody.data && listBody.data.components) || [];
                applyLoaded(results[1]);
                render();
            }).catch(function (err) {
                host.innerHTML = '<p role="alert">' + esc((err && err.message) || "Failed to load components") + "</p>";
            });
        }
        reload();
    }

    function bindGroupedPicker(root, opts) {
        opts = opts || {};
        var host = root && root.querySelector ? root.querySelector("[data-grouped-picker]") : null;
        if (!host || !opts.productID || typeof opts.api !== "function") {
            return;
        }
        if (host.getAttribute("data-composition-picker-bound") === "1") {
            return;
        }
        host.setAttribute("data-composition-picker-bound", "1");
        var leaves = openLeafSession(opts.api, opts.productID);
        var memberRows = [];
        var state = { products: [], assigned: {}, search: "", canWrite: !!opts.canWrite, hasMore: false };
        function setMessage(text, isError) {
            var el = host.querySelector("[data-grouped-msg]");
            if (!el) {
                return;
            }
            el.innerHTML = text ? '<span' + (isError ? ' role="alert"' : "") + ">" + esc(text) + "</span>" : "";
        }
        function bindListControls() {
            var toggles = host.querySelectorAll("[data-grouped-toggle]");
            for (var i = 0; i < toggles.length; i++) {
                toggles[i].addEventListener("change", onToggle);
            }
        }
        function renderList() {
            var list = host.querySelector("[data-grouped-list]");
            if (!list) {
                return;
            }
            list.innerHTML = renderGroupedListHTML(state);
            bindListControls();
        }
        function bindLoadMore() {
            var btn = host.querySelector("[data-grouped-load-more]");
            if (!btn) {
                return;
            }
            btn.addEventListener("click", onLoadMore);
        }
        function render() {
            host.innerHTML = renderGroupedPickerBody(state);
            var search = host.querySelector("[data-grouped-search]");
            if (search) {
                search.addEventListener("input", function () {
                    state.search = search.value || "";
                    renderList();
                });
            }
            bindListControls();
            bindLoadMore();
        }
        function applyLoaded(loaded) {
            var merged = productsWithAssigned(loaded, opts.productID, memberRows, function (row) {
                return row.member_product_id;
            });
            state.products = merged.products;
            state.assigned = {};
            for (var i = 0; i < merged.assigned.length; i++) {
                state.assigned[merged.assigned[i].id] = true;
            }
            state.hasMore = leaves.hasMore();
        }
        function onLoadMore() {
            leaves.more().then(function (loaded) {
                applyLoaded(loaded);
                renderList();
                var slot = host.querySelector("[data-grouped-more]");
                if (slot) {
                    slot.innerHTML = state.hasMore ? '<button type="button" data-grouped-load-more>Load more</button>' : "";
                }
                bindLoadMore();
            }).catch(function (err) {
                setMessage((err && err.message) || "Failed to load products", true);
            });
        }
        function showFailure(body) {
            var message = (body && body.error && body.error.message) || "Save failed";
            return reload().then(function () {
                setMessage(message, true);
            });
        }
        function onToggle(e) {
            if (!state.canWrite) {
                e.target.checked = !e.target.checked;
                return;
            }
            var id = e.target.getAttribute("data-grouped-toggle");
            var req = groupedAssignmentRequest(opts.productID, id, e.target.checked);
            var options = { method: req.method };
            if (req.body) {
                options.body = JSON.stringify(req.body);
            }
            opts.api(req.path, options).then(function (body) {
                if (body && body.error) {
                    return showFailure(body);
                }
                return reload();
            }).catch(function () {
                return showFailure(null);
            });
        }
        function reload() {
            host.innerHTML = "<p>Loading…</p>";
            return Promise.all([
                opts.api("/admin/products/" + encodeURIComponent(opts.productID) + "/grouped-members"),
                leaves.ensure()
            ]).then(function (results) {
                var listBody = results[0] || {};
                if (listBody.error) {
                    host.innerHTML = '<p role="alert">' + esc(listBody.error.message || "Failed to load members") + "</p>";
                    return;
                }
                memberRows = (listBody.data && listBody.data.members) || [];
                applyLoaded(results[1]);
                render();
            }).catch(function (err) {
                host.innerHTML = '<p role="alert">' + esc((err && err.message) || "Failed to load members") + "</p>";
            });
        }
        reload();
    }

    var api = {
        LEAF_TYPES: LEAF_TYPES,
        LEAF_PAGE_LIMIT: LEAF_PAGE_LIMIT,
        LEAF_MAX_PAGES: LEAF_MAX_PAGES,
        createLeafCache: createLeafCache,
        loadLeafType: loadLeafType,
        productsWithAssigned: productsWithAssigned,
        SECTION_BUNDLE: SECTION_BUNDLE,
        SECTION_GROUPED: SECTION_GROUPED,
        modesFromProduct: modesFromProduct,
        explodeEnabled: explodeEnabled,
        renderBundleModesHTML: renderBundleModesHTML,
        renderBundleSectionHTML: renderBundleSectionHTML,
        renderGroupedSectionHTML: renderGroupedSectionHTML,
        collectModes: collectModes,
        syncExplodeControl: syncExplodeControl,
        bindBundleModes: bindBundleModes,
        mergeLeafProducts: mergeLeafProducts,
        filterBySearch: filterBySearch,
        bundleAssignmentRequest: bundleAssignmentRequest,
        groupedAssignmentRequest: groupedAssignmentRequest,
        renderBundlePickerBody: renderBundlePickerBody,
        renderGroupedPickerBody: renderGroupedPickerBody,
        loadLeafProducts: loadLeafProducts,
        bindBundlePicker: bindBundlePicker,
        bindGroupedPicker: bindGroupedPicker
    };

    root.ShopandaProductCompositionSection = api;
    if (typeof module !== "undefined" && module.exports) {
        module.exports = api;
    }
})(typeof globalThis !== "undefined" ? globalThis : this);
