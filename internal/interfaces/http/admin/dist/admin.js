// admin.js — Admin SPA scaffold (vanilla JS, no build step)
(function () {
    "use strict";

    var TOKEN_KEY = "shopanda_admin_token";
    var API_BASE = "/api/v1";

    // --- Auth helpers ---

    function getToken() {
        return localStorage.getItem(TOKEN_KEY);
    }

    function setToken(token) {
        localStorage.setItem(TOKEN_KEY, token);
    }

    function clearToken() {
        localStorage.removeItem(TOKEN_KEY);
    }

    function isAuthenticated() {
        return !!getToken();
    }

    function buildHeaders(headers) {
        var out = headers || {};
        var token = getToken();
        if (token) {
            out.Authorization = "Bearer " + token;
        }
        return out;
    }

    function api(url, options) {
        options = options || {};
        var headers = buildHeaders(options.headers || {});
        headers["Content-Type"] = "application/json";
        options.headers = headers;
        return fetch(API_BASE + url, options).then(function (res) {
            if (res.status === 401) {
                clearToken();
                navigateTo("/admin");
                return Promise.reject(new Error("unauthorized"));
            }
            return res.json();
        });
    }

    function uploadAsset(file, onProgress) {
        return new Promise(function (resolve, reject) {
            var xhr = new XMLHttpRequest();
            xhr.open("POST", API_BASE + "/admin/media", true);
            var token = getToken();
            if (token) {
                xhr.setRequestHeader("Authorization", "Bearer " + token);
            }
            xhr.upload.addEventListener("progress", function (e) {
                if (onProgress && e.lengthComputable) {
                    onProgress(Math.round((e.loaded / e.total) * 100));
                }
            });
            xhr.onload = function () {
                var body = {};
                try {
                    body = xhr.responseText ? JSON.parse(xhr.responseText) : {};
                } catch (err) {
                    reject(err);
                    return;
                }
                if (xhr.status === 401) {
                    clearToken();
                    navigateTo("/admin");
                    reject(new Error("unauthorized"));
                    return;
                }
                if (xhr.status < 200 || xhr.status >= 300) {
                    reject(body);
                    return;
                }
                resolve(body);
            };
            xhr.onerror = function () {
                reject(new Error("upload failed"));
            };
            var formData = new FormData();
            formData.append("file", file);
            xhr.send(formData);
        });
    }

    // --- Routing ---

    var routes = {
        "/admin": { title: "Login", render: renderLogin, auth: false },
        "/admin/dashboard": { title: "Dashboard", render: renderDashboard, auth: true },
        "/admin/products": { title: "Products", render: renderProductsGrid, auth: true },
        "/admin/orders": { title: "Orders", render: renderOrdersGrid, auth: true },
        "/admin/media": { title: "Media", render: renderMediaLibrary, auth: true },
        "/admin/settings": { title: "Settings", render: renderSettingsPage, auth: true }
    };

    function resolveRoute(path) {
        if (routes[path]) {
            return routes[path];
        }
        if (path === "/admin/products/new") {
            return { title: "New Product", render: renderProductCreate, auth: true };
        }
        var productMatch = path.match(/^\/admin\/products\/([^/]+)$/);
        if (productMatch) {
            var productID = decodeURIComponent(productMatch[1]);
            return {
                title: "Edit Product",
                auth: true,
                render: function (container) { renderProductEdit(container, productID); }
            };
        }
        var orderMatch = path.match(/^\/admin\/orders\/([^/]+)$/);
        if (orderMatch) {
            var orderID = decodeURIComponent(orderMatch[1]);
            return {
                title: "Order Detail",
                auth: true,
                render: function (container) { renderOrderDetail(container, orderID); }
            };
        }
        return routes["/admin/dashboard"];
    }
    // --- Product Grid ---
    function renderProductsGrid(container) {
        container.innerHTML = '<h2>Products</h2><div id="products-grid"></div>';
        var gridBox = document.getElementById("products-grid");
        Promise.all([
            api("/admin/grids/product.grid"),
            api("/admin/products?page=1&per_page=20&sort=created_at&order=desc")
        ]).then(function (results) {
            var grid = results[0] && results[0].data && results[0].data.grid;
            var productsRaw = results[1] && results[1].data && results[1].data.products;
            var products = normalizeProducts(productsRaw);
            if (!grid || !Array.isArray(products)) {
                gridBox.innerHTML = '<p role="alert">Failed to load grid or data.</p>';
                return;
            }
            var html = '<div style="margin-bottom:1rem"><button id="new-product-btn">New Product</button></div>';
            html += '<table class="admin-table"><thead><tr>';
            for (var i = 0; i < grid.columns.length; i++) {
                html += '<th>' + esc(grid.columns[i].label || grid.columns[i].name) + '</th>';
            }
            html += '<th></th></tr></thead><tbody>';
            if (products.length === 0) {
                html += '<tr><td colspan="' + (grid.columns.length+1) + '">No products.</td></tr>';
            } else {
                for (var j = 0; j < products.length; j++) {
                    var p = products[j];
                    html += '<tr>';
                    for (var k = 0; k < grid.columns.length; k++) {
                        var col = grid.columns[k];
                        var val = p[col.name];
                        if ((col.name === "created_at" || col.name === "updated_at") && val) {
                            val = String(val).substring(0, 10);
                        }
                        html += '<td>' + esc(val == null ? '' : val) + '</td>';
                    }
                    html += '<td><a href="/admin/products/' + esc(p.id) + '" data-link>Edit</a></td>';
                    html += '</tr>';
                }
            }
            html += '</tbody></table>';
            gridBox.innerHTML = html;
            var newBtn = document.getElementById("new-product-btn");
            if (newBtn) newBtn.addEventListener("click", function() { navigateTo("/admin/products/new"); });
        }).catch(function () {
            gridBox.innerHTML = '<p role="alert">Failed to load products.</p>';
        });
    }

    function renderProductCreate(container) {
        renderProductForm(container, null);
    }

    function renderProductEdit(container, productID) {
        renderProductForm(container, productID);
    }

    function renderProductForm(container, productID) {
        var title = productID ? "Edit Product" : "New Product";
        container.innerHTML =
            '<h2>' + title + '</h2>' +
            '<p><a href="/admin/products" data-link>Back to products</a></p>' +
            '<div id="product-form-msg"></div>' +
            '<form id="product-form"></form>' +
            '<section id="variant-panel" style="display:none; margin-top:2rem;">' +
            '<h3>Variants</h3>' +
            '<div id="variant-msg"></div>' +
            '<div id="variant-list"></div>' +
            '<form id="variant-create-form" class="variant-inline" style="margin-top:1rem;">' +
            '<label>SKU<input name="sku" required></label>' +
            '<label>Name<input name="name"></label>' +
            '<label>Weight<input name="weight" type="number" step="0.01" min="0"></label>' +
            '<button type="submit">Add Variant</button>' +
            '</form>' +
            '</section>';

        var msg = document.getElementById("product-form-msg");
        var form = document.getElementById("product-form");

        var requests = [api("/admin/forms/product.form")];
        if (productID) {
            requests.push(api("/products/" + encodeURIComponent(productID)));
        }

        Promise.all(requests).then(function (results) {
            var schema = results[0] && results[0].data && results[0].data.form;
            var product = null;
            if (productID) {
                product = normalizeProduct(results[1] && results[1].data && results[1].data.product);
            }
            if (!schema || !schema.fields) {
                msg.innerHTML = '<p role="alert">Failed to load form schema.</p>';
                return;
            }

            var html = "";
            for (var i = 0; i < schema.fields.length; i++) {
                html += renderSchemaField(schema.fields[i], product);
            }
            html += renderProductMediaField(product);
            html += '<button type="submit">' + (productID ? 'Save Product' : 'Create Product') + '</button>';
            form.innerHTML = html;

            setupProductMediaPicker(form, product);

            form.addEventListener("submit", function (e) {
                e.preventDefault();
                var payload = collectProductPayload(schema.fields, form);
                var method = productID ? "PUT" : "POST";
                var url = productID ? "/admin/products/" + encodeURIComponent(productID) : "/admin/products";
                api(url, { method: method, body: JSON.stringify(payload) }).then(function (body) {
                    if (body && body.error) {
                        msg.innerHTML = '<p role="alert">' + esc(body.error.message || "Save failed") + '</p>';
                        return;
                    }
                    msg.innerHTML = '<p>Saved.</p>';
                    if (!productID && body && body.data && body.data.product && body.data.product.id) {
                        navigateTo("/admin/products/" + body.data.product.id);
                    }
                }).catch(function () {
                    msg.innerHTML = '<p role="alert">Save failed.</p>';
                });
            });

            if (productID) {
                setupVariantPanel(productID);
            }
        }).catch(function () {
            msg.innerHTML = '<p role="alert">Failed to load product form.</p>';
        });
    }

    function renderSchemaField(field, product) {
        var name = field.name;
        var label = esc(field.label || name);
        var value = field.default;
        if (product && product[name] != null) {
            value = product[name];
        }
        if (value == null) {
            value = "";
        }

        if (field.type === "textarea") {
            return '<label>' + label + '<textarea name="' + esc(name) + '" ' + (field.required ? 'required' : '') + '>' + esc(String(value)) + '</textarea></label>';
        }
        if (field.type === "number") {
            return '<label>' + label + '<input type="number" name="' + esc(name) + '" value="' + esc(String(value)) + '" ' + (field.required ? 'required' : '') + '></label>';
        }
        if (field.type === "checkbox") {
            var checked = value ? 'checked' : '';
            return '<label><input type="checkbox" name="' + esc(name) + '" ' + checked + '> ' + label + '</label>';
        }
        if (field.type === "select") {
            var opts = '<label>' + label + '<select name="' + esc(name) + '">';
            var selected = String(value);
            var options = field.options || [];
            for (var i = 0; i < options.length; i++) {
                var o = options[i];
                var isSel = String(o.value) === selected ? ' selected' : '';
                opts += '<option value="' + esc(o.value) + '"' + isSel + '>' + esc(o.label) + '</option>';
            }
            opts += '</select></label>';
            return opts;
        }
        return '<label>' + label + '<input type="text" name="' + esc(name) + '" value="' + esc(String(value)) + '" ' + (field.required ? 'required' : '') + '></label>';
    }

    function collectProductPayload(fields, form) {
        var payload = { attributes: {} };
        for (var i = 0; i < fields.length; i++) {
            var f = fields[i];
            var el = form.elements[f.name];
            if (!el) {
                continue;
            }
            var v;
            if (f.type === "checkbox") {
                v = !!el.checked;
            } else {
                v = el.value;
            }

            if (f.name === "name" || f.name === "slug" || f.name === "description" || f.name === "status") {
                payload[f.name] = v;
            } else {
                payload.attributes[f.name] = v;
            }
        }
        if (form.elements.image_asset_id && (form.getAttribute("data-media-dirty") === "true" || form.elements.image_asset_id.value || form.elements.image_url.value)) {
            payload.attributes.image_asset_id = form.elements.image_asset_id.value;
            payload.attributes.image_url = form.elements.image_url.value;
        }
        if (Object.keys(payload.attributes).length === 0) {
            delete payload.attributes;
        }
        return payload;
    }

    function renderProductMediaField(product) {
        var attrs = product && product.attributes ? product.attributes : {};
        var assetID = attrs.image_asset_id || "";
        var imageURL = attrs.image_url || "";
        return '' +
            '<section class="product-media-field">' +
            '<h3>Featured Image</h3>' +
            '<input type="hidden" name="image_asset_id" value="' + esc(assetID) + '">' +
            '<input type="hidden" name="image_url" value="' + esc(imageURL) + '">' +
            '<div id="product-image-preview" class="product-image-preview">' + renderProductMediaPreview(assetID, imageURL) + '</div>' +
            '<div class="media-field-actions">' +
            '<button type="button" id="choose-product-image">Choose From Library</button>' +
            '<button type="button" id="clear-product-image" class="secondary">Clear</button>' +
            '</div>' +
            '</section>';
    }

    function renderProductMediaPreview(assetID, imageURL) {
        if (!assetID && !imageURL) {
            return '<p>No image selected.</p>';
        }
        var html = '';
        if (imageURL) {
            html += '<img src="' + esc(imageURL) + '" alt="Selected product image">';
        }
        html += '<p>Asset ID: ' + esc(assetID || 'n/a') + '</p>';
        return html;
    }

    function setupProductMediaPicker(form, product) {
        var chooseBtn = document.getElementById("choose-product-image");
        var clearBtn = document.getElementById("clear-product-image");
        var assetInput = form.elements.image_asset_id;
        var urlInput = form.elements.image_url;
        var preview = document.getElementById("product-image-preview");

        if (product && product.attributes && (product.attributes.image_asset_id || product.attributes.image_url)) {
            form.setAttribute("data-media-dirty", "true");
        }

        function updatePreview() {
            preview.innerHTML = renderProductMediaPreview(assetInput.value, urlInput.value);
        }

        chooseBtn.addEventListener("click", function () {
            openMediaPicker(function (asset) {
                assetInput.value = asset.id || "";
                urlInput.value = asset.url || "";
                form.setAttribute("data-media-dirty", "true");
                updatePreview();
            });
        });

        clearBtn.addEventListener("click", function () {
            assetInput.value = "";
            urlInput.value = "";
            form.setAttribute("data-media-dirty", "true");
            updatePreview();
        });
    }

    function setupVariantPanel(productID) {
        var panel = document.getElementById("variant-panel");
        panel.style.display = "block";

        function loadVariants() {
            var list = document.getElementById("variant-list");
            api("/products/" + encodeURIComponent(productID) + "/variants").then(function (body) {
                var variants = normalizeVariants(body && body.data && body.data.variants ? body.data.variants : []);
                renderVariants(list, productID, variants, loadVariants);
            }).catch(function () {
                list.innerHTML = '<p role="alert">Failed to load variants.</p>';
            });
        }

        var createForm = document.getElementById("variant-create-form");
        var msg = document.getElementById("variant-msg");
        createForm.addEventListener("submit", function (e) {
            e.preventDefault();
            var payload = {
                sku: createForm.elements.sku.value,
                name: createForm.elements.name.value
            };
            var w = createForm.elements.weight.value;
            if (w !== "") {
                payload.weight = Number(w);
            }
            api("/admin/products/" + encodeURIComponent(productID) + "/variants", {
                method: "POST",
                body: JSON.stringify(payload)
            }).then(function (body) {
                if (body && body.error) {
                    msg.innerHTML = '<p role="alert">' + esc(body.error.message || "Create variant failed") + '</p>';
                    return;
                }
                msg.innerHTML = '<p>Variant added.</p>';
                createForm.reset();
                loadVariants();
            }).catch(function () {
                msg.innerHTML = '<p role="alert">Create variant failed.</p>';
            });
        });

        loadVariants();
    }

    function renderVariants(container, productID, variants, reload) {
        if (!variants || variants.length === 0) {
            container.innerHTML = '<p>No variants yet.</p>';
            return;
        }
        var html = '<table><thead><tr><th>SKU</th><th>Name</th><th>Weight</th><th></th></tr></thead><tbody>';
        for (var i = 0; i < variants.length; i++) {
            var v = variants[i];
            html += '<tr data-variant-id="' + esc(v.id) + '">';
            html += '<td><input data-field="sku" value="' + esc(v.sku || '') + '"></td>';
            html += '<td><input data-field="name" value="' + esc(v.name || '') + '"></td>';
            html += '<td><input data-field="weight" type="number" step="0.01" min="0" value="' + esc(v.weight == null ? '' : String(v.weight)) + '"></td>';
            html += '<td><button type="button" class="variant-save-btn">Save</button></td>';
            html += '</tr>';
        }
        html += '</tbody></table>';
        container.innerHTML = html;

        var buttons = container.querySelectorAll(".variant-save-btn");
        for (var j = 0; j < buttons.length; j++) {
            buttons[j].addEventListener("click", function (e) {
                var row = e.target.closest("tr");
                var variantID = row.getAttribute("data-variant-id");
                var sku = row.querySelector('[data-field="sku"]').value;
                var name = row.querySelector('[data-field="name"]').value;
                var weightRaw = row.querySelector('[data-field="weight"]').value;
                var payload = { sku: sku, name: name };
                if (weightRaw !== "") {
                    payload.weight = Number(weightRaw);
                }
                api("/admin/products/" + encodeURIComponent(productID) + "/variants/" + encodeURIComponent(variantID), {
                    method: "PUT",
                    body: JSON.stringify(payload)
                }).then(function (body) {
                    if (body && body.error) {
                        return;
                    }
                    reload();
                });
            });
        }
    }

    function normalizeProducts(products) {
        if (!Array.isArray(products)) {
            return [];
        }
        var out = [];
        for (var i = 0; i < products.length; i++) {
            out.push(normalizeProduct(products[i]));
        }
        return out;
    }

    function normalizeProduct(raw) {
        if (!raw) {
            return null;
        }
        return {
            id: pick(raw, "id", "ID"),
            name: pick(raw, "name", "Name"),
            slug: pick(raw, "slug", "Slug"),
            description: pick(raw, "description", "Description"),
            status: pick(raw, "status", "Status"),
            attributes: pick(raw, "attributes", "Attributes") || {},
            created_at: pick(raw, "created_at", "CreatedAt"),
            updated_at: pick(raw, "updated_at", "UpdatedAt")
        };
    }

    function normalizeVariants(variants) {
        if (!Array.isArray(variants)) {
            return [];
        }
        var out = [];
        for (var i = 0; i < variants.length; i++) {
            var v = variants[i] || {};
            out.push({
                id: pick(v, "id", "ID"),
                sku: pick(v, "sku", "SKU"),
                name: pick(v, "name", "Name"),
                weight: pick(v, "weight", "Weight")
            });
        }
        return out;
    }

    function normalizeOrders(orders) {
        if (!Array.isArray(orders)) {
            return [];
        }
        var out = [];
        for (var i = 0; i < orders.length; i++) {
            var order = normalizeOrder(orders[i]);
            if (order) {
                out.push(order);
            }
        }
        return out;
    }

    function normalizeStores(stores) {
        if (!Array.isArray(stores)) {
            return [];
        }
        var out = [];
        for (var i = 0; i < stores.length; i++) {
            var store = normalizeStore(stores[i]);
            if (store) {
                out.push(store);
            }
        }
        return out;
    }

    function normalizeStore(raw) {
        if (!raw) {
            return null;
        }
        return {
            id: pick(raw, "id", "ID"),
            code: pick(raw, "code", "Code"),
            name: pick(raw, "name", "Name"),
            currency: pick(raw, "currency", "Currency"),
            country: pick(raw, "country", "Country"),
            language: pick(raw, "language", "Language"),
            domain: pick(raw, "domain", "Domain"),
            is_default: !!pick(raw, "is_default", "IsDefault")
        };
    }

    function normalizeOrder(raw) {
        if (!raw) {
            return null;
        }
        return {
            id: pick(raw, "id", "ID"),
            customer_id: pick(raw, "customer_id", "CustomerID"),
            status: pick(raw, "status", "Status"),
            currency: pick(raw, "currency", "Currency"),
            total_amount: pick(raw, "total_amount", "TotalAmount"),
            created_at: pick(raw, "created_at", "CreatedAt"),
            updated_at: pick(raw, "updated_at", "UpdatedAt"),
            items: normalizeOrderItems(pick(raw, "items", "Items"))
        };
    }

    function normalizeOrderItems(items) {
        if (!Array.isArray(items)) {
            return [];
        }
        var out = [];
        for (var i = 0; i < items.length; i++) {
            var it = items[i] || {};
            out.push({
                variant_id: pick(it, "variant_id", "VariantID"),
                sku: pick(it, "sku", "SKU"),
                name: pick(it, "name", "Name"),
                quantity: pick(it, "quantity", "Quantity"),
                unit_price: pick(it, "unit_price", "UnitPrice"),
                currency: pick(it, "currency", "Currency")
            });
        }
        return out;
    }

    function normalizeAssets(assets) {
        if (!Array.isArray(assets)) {
            return [];
        }
        var out = [];
        for (var i = 0; i < assets.length; i++) {
            var a = assets[i] || {};
            out.push({
                id: pick(a, "id", "ID"),
                path: pick(a, "path", "Path"),
                filename: pick(a, "filename", "Filename"),
                mime_type: pick(a, "mime_type", "MimeType"),
                size: pick(a, "size", "Size"),
                url: pick(a, "url", "URL"),
                thumbnails: pick(a, "thumbnails", "Thumbnails") || {},
                created_at: pick(a, "created_at", "CreatedAt")
            });
        }
        return out;
    }

    function pick(obj, a, b) {
        if (!obj) {
            return undefined;
        }
        if (obj[a] != null) {
            return obj[a];
        }
        if (obj[b] != null) {
            return obj[b];
        }
        return undefined;
    }

    function navigateTo(path) {
        history.pushState(null, "", path);
        handleRoute();
    }

    function handleRoute() {
        var path = location.pathname;
        var route = resolveRoute(path);
        if (!route) return;

        if (route.auth && !isAuthenticated()) {
            navigateTo("/admin");
            return;
        }

        if (!route.auth && path === "/admin" && isAuthenticated()) {
            navigateTo("/admin/dashboard");
            return;
        }

        document.title = route.title + " — Admin";
        var layout = document.getElementById("admin-layout");
        layout.setAttribute("data-auth", isAuthenticated() ? "true" : "false");

        updateSidebar(path);
        updateUserInfo();

        var content = document.getElementById("admin-content");
        content.innerHTML = "";
        route.render(content);
    }

    function updateSidebar(currentPath) {
        var links = document.querySelectorAll(".admin-sidebar nav a");
        for (var i = 0; i < links.length; i++) {
            var href = links[i].getAttribute("href");
            if (href === "/admin/products" && currentPath.indexOf("/admin/products") === 0) {
                links[i].setAttribute("aria-current", "page");
            } else if (href === "/admin/orders" && currentPath.indexOf("/admin/orders") === 0) {
                links[i].setAttribute("aria-current", "page");
            } else if (href === "/admin/media" && currentPath.indexOf("/admin/media") === 0) {
                links[i].setAttribute("aria-current", "page");
            } else if (href === currentPath) {
                links[i].setAttribute("aria-current", "page");
            } else {
                links[i].removeAttribute("aria-current");
            }
        }
    }

    function updateUserInfo() {
        var el = document.getElementById("admin-user-name");
        if (!el) return;
        if (isAuthenticated()) {
            el.textContent = "Admin";
        }
    }

    // --- Pages ---

    function renderLogin(container) {
        var html =
            '<div class="login-container">' +
            "<h1>Admin Login</h1>" +
            '<form id="login-form">' +
            "<label>Email<input type=\"email\" name=\"email\" required autocomplete=\"username\"></label>" +
            "<label>Password<input type=\"password\" name=\"password\" required autocomplete=\"current-password\"></label>" +
            '<div id="login-error" role="alert"></div>' +
            "<button type=\"submit\">Sign In</button>" +
            "</form>" +
            "</div>";
        container.innerHTML = html;

        var form = document.getElementById("login-form");
        form.addEventListener("submit", function (e) {
            e.preventDefault();
            var email = form.elements.email.value;
            var password = form.elements.password.value;
            var errBox = document.getElementById("login-error");
            errBox.textContent = "";

            fetch(API_BASE + "/auth/login", {
                method: "POST",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify({ email: email, password: password })
            })
                .then(function (res) { return res.json().then(function (body) { return { status: res.status, body: body }; }); })
                .then(function (result) {
                    if (result.status !== 200 || !result.body.data || !result.body.data.token) {
                        errBox.textContent = (result.body.error && result.body.error.message) || "Login failed";
                        return;
                    }
                    setToken(result.body.data.token);
                    navigateTo("/admin/dashboard");
                })
                .catch(function () {
                    errBox.textContent = "Network error";
                });
        });
    }

    function renderDashboard(container) {
        container.innerHTML =
            '<h2>Dashboard</h2>' +
            '<div class="stats-cards">' +
            '  <article class="stat-card" id="stat-orders"><header>Orders Today</header><p>—</p></article>' +
            '  <article class="stat-card" id="stat-revenue"><header>Revenue Today</header><p>—</p></article>' +
            '  <article class="stat-card" id="stat-products"><header>Total Products</header><p>—</p></article>' +
            '  <article class="stat-card" id="stat-lowstock"><header>Low Stock</header><p>—</p></article>' +
            '</div>' +
            '<h3>Recent Orders</h3>' +
            '<table id="recent-orders"><thead><tr>' +
            '<th>ID</th><th>Customer</th><th>Total</th><th>Status</th><th>Date</th>' +
            '</tr></thead><tbody><tr><td colspan="5">Loading…</td></tr></tbody></table>';

        api("/admin/stats/overview").then(function (body) {
            if (!body || !body.data) return;
            var d = body.data;

            setStat("stat-orders", d.orders_today);
            setStat("stat-revenue", formatMoney(d.revenue_today.amount, d.revenue_today.currency));
            setStat("stat-products", d.total_products);
            setStat("stat-lowstock", d.low_stock_count);

            var tbody = document.querySelector("#recent-orders tbody");
            if (!d.recent_orders || d.recent_orders.length === 0) {
                tbody.innerHTML = '<tr><td colspan="5">No orders yet.</td></tr>';
                return;
            }
            var rows = "";
            for (var i = 0; i < d.recent_orders.length; i++) {
                var o = d.recent_orders[i];
                rows +=
                    "<tr>" +
                    "<td>" + esc(o.id.substring(0, 8)) + "</td>" +
                    "<td>" + esc(o.customer_id.substring(0, 8)) + "</td>" +
                    "<td>" + formatMoney(o.total_amount, o.currency) + "</td>" +
                    "<td><span class=\"badge badge-" + esc(o.status) + "\">" + esc(o.status) + "</span></td>" +
                    "<td>" + esc(o.created_at.substring(0, 10)) + "</td>" +
                    "</tr>";
            }
            tbody.innerHTML = rows;
        }).catch(function () {
            container.innerHTML = '<h2>Dashboard</h2><p role="alert">Failed to load dashboard data.</p>';
        });
    }

    function renderOrdersGrid(container) {
        container.innerHTML =
            '<h2>Orders</h2>' +
            '<label>Status Filter<select id="orders-status-filter">' +
            '<option value="">All</option>' +
            '<option value="pending">pending</option>' +
            '<option value="confirmed">confirmed</option>' +
            '<option value="paid">paid</option>' +
            '<option value="cancelled">cancelled</option>' +
            '<option value="failed">failed</option>' +
            '</select></label>' +
            '<div id="orders-grid"></div>';

        var grid = document.getElementById("orders-grid");
        var filter = document.getElementById("orders-status-filter");
        var allOrders = [];

        function renderRows() {
            var selected = filter.value;
            var orders = allOrders;
            if (selected) {
                var filtered = [];
                for (var i = 0; i < allOrders.length; i++) {
                    if (allOrders[i].status === selected) {
                        filtered.push(allOrders[i]);
                    }
                }
                orders = filtered;
            }

            var html = '<table><thead><tr>' +
                '<th>ID</th><th>Customer</th><th>Total</th><th>Status</th><th>Payment</th><th>Date</th><th></th>' +
                '</tr></thead><tbody>';

            if (orders.length === 0) {
                html += '<tr><td colspan="7">No orders found.</td></tr>';
            } else {
                for (var j = 0; j < orders.length; j++) {
                    var o = orders[j];
                    html += '<tr>' +
                        '<td>' + esc(o.id) + '</td>' +
                        '<td>' + esc(o.customer_id || '') + '</td>' +
                        '<td>' + formatMoney(Number(o.total_amount || 0), o.currency) + '</td>' +
                        '<td><span class="badge badge-' + esc(o.status) + '">' + esc(o.status) + '</span></td>' +
                        '<td>' + esc(derivePaymentStatus(o.status)) + '</td>' +
                        '<td>' + esc(o.created_at ? String(o.created_at).substring(0, 10) : '') + '</td>' +
                        '<td><a href="/admin/orders/' + esc(o.id) + '" data-link>View</a></td>' +
                        '</tr>';
                }
            }
            html += '</tbody></table>';
            grid.innerHTML = html;
        }

        filter.addEventListener("change", renderRows);

        api("/admin/orders?offset=0&limit=50").then(function (body) {
            allOrders = normalizeOrders(body && body.data && body.data.orders ? body.data.orders : []);
            renderRows();
        }).catch(function () {
            grid.innerHTML = '<p role="alert">Failed to load orders.</p>';
        });
    }

    function renderMediaLibrary(container) {
        container.innerHTML =
            '<div class="media-toolbar">' +
            '<h2>Media Library</h2>' +
            '<input type="file" id="media-upload-input" accept="image/*" multiple>' +
            '</div>' +
            '<div id="media-msg"></div>' +
            '<div id="media-dropzone" class="media-dropzone">Drop images here or use the file picker.</div>' +
            '<progress id="media-upload-progress" value="0" max="100" style="display:none"></progress>' +
            '<div id="media-grid" class="media-grid"></div>';

        setupMediaManager({
            messageID: "media-msg",
            gridID: "media-grid",
            fileInputID: "media-upload-input",
            dropzoneID: "media-dropzone",
            progressID: "media-upload-progress"
        });
    }

    function renderSettingsPage(container) {
        container.innerHTML =
            '<h2>Settings</h2>' +
            '<div id="settings-global-msg"></div>' +
            '<div class="settings-grid">' +
            '<section><h3>Store Info</h3><div id="settings-store-msg"></div><form id="settings-store-form"></form></section>' +
            '<section><h3>Email</h3><div id="settings-email-msg"></div><form id="settings-email-form"></form></section>' +
            '<section><h3>Media</h3><div id="settings-media-msg"></div><form id="settings-media-form"></form></section>' +
            '<section><h3>Currency</h3><div id="settings-currency-msg"></div><form id="settings-currency-form"></form></section>' +
            '<section><h3>Tax</h3><div id="settings-tax-msg"></div><form id="settings-tax-form"></form></section>' +
            '</div>';

        Promise.all([
            api('/admin/stores'),
            api('/admin/config?group=store'),
            api('/admin/config?group=email'),
            api('/admin/config?group=media'),
            api('/admin/config?group=currency'),
            api('/admin/config?group=tax')
        ]).then(function (results) {
            var stores = normalizeStores(results[0] && results[0].data && results[0].data.stores ? results[0].data.stores : []);
            var storeSettings = results[1] && results[1].data && results[1].data.entries ? results[1].data.entries : {};
            var emailSettings = results[2] && results[2].data && results[2].data.entries ? results[2].data.entries : {};
            var mediaSettings = results[3] && results[3].data && results[3].data.entries ? results[3].data.entries : {};
            var currencySettings = results[4] && results[4].data && results[4].data.entries ? results[4].data.entries : {};
            var taxSettings = results[5] && results[5].data && results[5].data.entries ? results[5].data.entries : {};

            renderStoreSettingsForm(container, choosePrimaryStore(stores), storeSettings);
            renderEmailSettingsForm(container, emailSettings);
            renderMediaSettingsForm(container, mediaSettings);
            renderCurrencySettingsForm(container, currencySettings);
            renderTaxSettingsForm(container, taxSettings);
        }).catch(function () {
            container.innerHTML = '<h2>Settings</h2><p role="alert">Failed to load settings.</p>';
        });
    }

    function choosePrimaryStore(stores) {
        if (!stores || stores.length === 0) {
            return null;
        }
        for (var i = 0; i < stores.length; i++) {
            if (stores[i].is_default) {
                return stores[i];
            }
        }
        return stores[0];
    }

    function renderStoreSettingsForm(container, store, storeSettings) {
        var form = document.getElementById('settings-store-form');
        form.innerHTML = '' +
            '<label>Code<input name="code" value="' + esc(store ? store.code : '') + '" required></label>' +
            '<label>Name<input name="name" value="' + esc(store ? store.name : '') + '" required></label>' +
            '<label>Domain / URL<input name="domain" value="' + esc(store ? store.domain : '') + '"></label>' +
            '<label>Country<input name="country" value="' + esc(store ? store.country : '') + '" required></label>' +
            '<label>Language<input name="language" value="' + esc(store ? store.language : '') + '" required></label>' +
            '<label>Currency<input name="currency" value="' + esc(store ? store.currency : '') + '" required></label>' +
            '<label>Address<textarea name="store_address">' + esc(valueOf(storeSettings, 'store.address', '')) + '</textarea></label>' +
            '<label>Logo URL<input name="store_logo" value="' + esc(valueOf(storeSettings, 'store.logo', '')) + '"></label>' +
            '<label><input type="checkbox" name="is_default" ' + (store && store.is_default ? 'checked' : '') + '> Default store</label>' +
            '<button type="submit">Save Store Info</button>';

        form.addEventListener('submit', function (e) {
            e.preventDefault();
            var payload = {
                code: form.elements.code.value,
                name: form.elements.name.value,
                domain: form.elements.domain.value,
                country: form.elements.country.value,
                language: form.elements.language.value,
                currency: form.elements.currency.value,
                is_default: !!form.elements.is_default.checked
            };
            var configPayload = {
                entries: {
                    'store.address': form.elements.store_address.value,
                    'store.logo': form.elements.store_logo.value
                }
            };
            var storeReq;
            if (store && store.id) {
                storeReq = api('/admin/stores/' + encodeURIComponent(store.id), {
                    method: 'PUT',
                    body: JSON.stringify(payload)
                });
            } else {
                storeReq = api('/admin/stores', {
                    method: 'POST',
                    body: JSON.stringify(payload)
                });
            }
            Promise.all([
                storeReq,
                api('/admin/config', { method: 'PUT', body: JSON.stringify(configPayload) })
            ]).then(function (responses) {
                if ((responses[0] && responses[0].error) || (responses[1] && responses[1].error)) {
                    setSettingsMessage('settings-store-msg', extractErrorMessage(responses[0] || responses[1], 'Failed to save store settings.'), true);
                    return;
                }
                setSettingsMessage('settings-store-msg', 'Store settings saved.', false);
                renderSettingsPage(container);
            }).catch(function (err) {
                setSettingsMessage('settings-store-msg', extractErrorMessage(err, 'Failed to save store settings.'), true);
            });
        });
    }

    function renderEmailSettingsForm(container, settings) {
        var form = document.getElementById('settings-email-form');
        form.innerHTML = '' +
            '<label>SMTP Host<input name="host" value="' + esc(valueOf(settings, 'mail.smtp.host', '')) + '"></label>' +
            '<label>SMTP Port<input name="port" type="number" min="1" value="' + esc(String(valueOf(settings, 'mail.smtp.port', 0) || '')) + '"></label>' +
            '<label>SMTP User<input name="user" value="' + esc(valueOf(settings, 'mail.smtp.user', '')) + '"></label>' +
            '<label>SMTP Password<input name="password" type="password" value="' + esc(valueOf(settings, 'mail.smtp.password', '')) + '"></label>' +
            '<label>From Address<input name="from" value="' + esc(valueOf(settings, 'mail.smtp.from', '')) + '"></label>' +
            '<label>Test Recipient<input name="test_to" type="email" placeholder="merchant@example.com"></label>' +
            '<div class="settings-actions">' +
            '<button type="submit">Save Email Settings</button>' +
            '<button type="button" id="settings-test-email" class="secondary">Send Test Email</button>' +
            '</div>';

        form.addEventListener('submit', function (e) {
            e.preventDefault();
            saveSettingsEntries('settings-email-msg', {
                'mail.smtp.host': form.elements.host.value,
                'mail.smtp.port': Number(form.elements.port.value || 0),
                'mail.smtp.user': form.elements.user.value,
                'mail.smtp.password': form.elements.password.value,
                'mail.smtp.from': form.elements.from.value
            });
        });

        document.getElementById('settings-test-email').addEventListener('click', function () {
            api('/admin/config/test-email', {
                method: 'POST',
                body: JSON.stringify({
                    to: form.elements.test_to.value,
                    host: form.elements.host.value,
                    port: Number(form.elements.port.value || 0),
                    user: form.elements.user.value,
                    password: form.elements.password.value,
                    from: form.elements.from.value
                })
            }).then(function (body) {
                if (body && body.error) {
                    setSettingsMessage('settings-email-msg', body.error.message || 'Failed to send test email.', true);
                    return;
                }
                setSettingsMessage('settings-email-msg', 'Test email sent.', false);
            }).catch(function (err) {
                setSettingsMessage('settings-email-msg', extractErrorMessage(err, 'Failed to send test email.'), true);
            });
        });
    }

    function renderMediaSettingsForm(container, settings) {
        var form = document.getElementById('settings-media-form');
        form.innerHTML = '' +
            '<label>Storage<select name="storage">' +
            renderSelectOptions(['local', 's3'], valueOf(settings, 'media.storage', 'local')) +
            '</select></label>' +
            '<label>Local Base Path<input name="local_base_path" value="' + esc(valueOf(settings, 'media.local.base_path', '')) + '"></label>' +
            '<label>Local Base URL<input name="local_base_url" value="' + esc(valueOf(settings, 'media.local.base_url', '')) + '"></label>' +
            '<label>S3 Endpoint<input name="s3_endpoint" value="' + esc(valueOf(settings, 'media.s3.endpoint', '')) + '"></label>' +
            '<label>S3 Bucket<input name="s3_bucket" value="' + esc(valueOf(settings, 'media.s3.bucket', '')) + '"></label>' +
            '<label>S3 Region<input name="s3_region" value="' + esc(valueOf(settings, 'media.s3.region', '')) + '"></label>' +
            '<label>S3 Base URL<input name="s3_base_url" value="' + esc(valueOf(settings, 'media.s3.base_url', '')) + '"></label>' +
            '<label><input type="checkbox" name="s3_public_acl" ' + (truthy(valueOf(settings, 'media.s3.public_acl', false)) ? 'checked' : '') + '> S3 Public ACL</label>' +
            '<button type="submit">Save Media Settings</button>';

        form.addEventListener('submit', function (e) {
            e.preventDefault();
            saveSettingsEntries('settings-media-msg', {
                'media.storage': form.elements.storage.value,
                'media.local.base_path': form.elements.local_base_path.value,
                'media.local.base_url': form.elements.local_base_url.value,
                'media.s3.endpoint': form.elements.s3_endpoint.value,
                'media.s3.bucket': form.elements.s3_bucket.value,
                'media.s3.region': form.elements.s3_region.value,
                'media.s3.base_url': form.elements.s3_base_url.value,
                'media.s3.public_acl': !!form.elements.s3_public_acl.checked
            });
        });
    }

    function renderCurrencySettingsForm(container, settings) {
        var form = document.getElementById('settings-currency-form');
        form.innerHTML = '' +
            '<label>Default Currency<input name="default_currency" value="' + esc(valueOf(settings, 'default_currency', 'EUR')) + '"></label>' +
            '<label>Display Format<input name="display_format" value="' + esc(valueOf(settings, 'currency.display_format', '{currency} {amount}')) + '"></label>' +
            '<button type="submit">Save Currency Settings</button>';
        form.addEventListener('submit', function (e) {
            e.preventDefault();
            saveSettingsEntries('settings-currency-msg', {
                'default_currency': form.elements.default_currency.value,
                'currency.display_format': form.elements.display_format.value
            });
        });
    }

    function renderTaxSettingsForm(container, settings) {
        var form = document.getElementById('settings-tax-form');
        form.innerHTML = '' +
            '<label>Default Tax Class<input name="default_class" value="' + esc(valueOf(settings, 'tax.default_class', 'standard')) + '"></label>' +
            '<label><input type="checkbox" name="tax_included" ' + (truthy(valueOf(settings, 'tax.included', false)) ? 'checked' : '') + '> Prices Include Tax</label>' +
            '<button type="submit">Save Tax Settings</button>';
        form.addEventListener('submit', function (e) {
            e.preventDefault();
            saveSettingsEntries('settings-tax-msg', {
                'tax.default_class': form.elements.default_class.value,
                'tax.included': !!form.elements.tax_included.checked
            });
        });
    }

    function saveSettingsEntries(messageID, entries) {
        api('/admin/config', {
            method: 'PUT',
            body: JSON.stringify({ entries: entries })
        }).then(function (body) {
            if (body && body.error) {
                setSettingsMessage(messageID, body.error.message || 'Failed to save settings.', true);
                return;
            }
            setSettingsMessage(messageID, 'Settings saved.', false);
        }).catch(function (err) {
            setSettingsMessage(messageID, extractErrorMessage(err, 'Failed to save settings.'), true);
        });
    }

    function setSettingsMessage(id, message, isError) {
        var el = document.getElementById(id);
        if (!el) {
            return;
        }
        el.innerHTML = '<p' + (isError ? ' role="alert"' : '') + '>' + esc(message) + '</p>';
    }

    function renderSelectOptions(values, selected) {
        var html = '';
        for (var i = 0; i < values.length; i++) {
            html += '<option value="' + esc(values[i]) + '"' + (String(values[i]) === String(selected) ? ' selected' : '') + '>' + esc(values[i]) + '</option>';
        }
        return html;
    }

    function valueOf(obj, key, fallback) {
        if (obj && obj[key] != null) {
            return obj[key];
        }
        return fallback;
    }

    function truthy(value) {
        return value === true || value === 'true' || value === 1 || value === '1';
    }

    function setupMediaManager(options) {
        var msg = document.getElementById(options.messageID);
        var grid = document.getElementById(options.gridID);
        var fileInput = document.getElementById(options.fileInputID);
        var dropzone = document.getElementById(options.dropzoneID);
        var progress = document.getElementById(options.progressID);

        function showMessage(text, isError) {
            msg.innerHTML = text ? '<p' + (isError ? ' role="alert"' : '') + '>' + esc(text) + '</p>' : '';
        }

        function loadAssets() {
            api("/admin/media?offset=0&limit=100").then(function (body) {
                var assets = normalizeAssets(body && body.data && body.data.assets ? body.data.assets : []);
                renderMediaAssets(grid, assets, options.onSelect, loadAssets, showMessage);
            }).catch(function () {
                grid.innerHTML = '<p role="alert">Failed to load media library.</p>';
            });
        }

        function handleFiles(files) {
            if (!files || files.length === 0) {
                return;
            }
            var queue = Array.prototype.slice.call(files);
            progress.style.display = "block";
            progress.value = 0;

            function next(index) {
                if (index >= queue.length) {
                    progress.style.display = "none";
                    progress.value = 0;
                    fileInput.value = "";
                    loadAssets();
                    showMessage(queue.length + ' file(s) uploaded.', false);
                    return;
                }
                uploadAsset(queue[index], function (percent) {
                    progress.value = percent;
                }).then(function () {
                    next(index + 1);
                }).catch(function (err) {
                    progress.style.display = "none";
                    showMessage(extractErrorMessage(err, 'Upload failed.'), true);
                });
            }

            next(0);
        }

        fileInput.addEventListener("change", function () {
            handleFiles(fileInput.files);
        });

        dropzone.addEventListener("dragover", function (e) {
            e.preventDefault();
            dropzone.classList.add("is-dragover");
        });
        dropzone.addEventListener("dragleave", function () {
            dropzone.classList.remove("is-dragover");
        });
        dropzone.addEventListener("drop", function (e) {
            e.preventDefault();
            dropzone.classList.remove("is-dragover");
            handleFiles(e.dataTransfer.files);
        });

        loadAssets();
    }

    function renderMediaAssets(container, assets, onSelect, reload, showMessage) {
        if (!assets || assets.length === 0) {
            container.innerHTML = '<p>No media uploaded yet.</p>';
            return;
        }

        var html = '';
        for (var i = 0; i < assets.length; i++) {
            var asset = assets[i];
            var previewURL = asset.thumbnails.small || asset.thumbnails.medium || asset.url;
            html += '' +
                '<article class="media-card" data-asset-id="' + esc(asset.id) + '">' +
                '<div class="media-card-preview">' +
                (previewURL ? '<img src="' + esc(previewURL) + '" alt="' + esc(asset.filename || 'asset') + '">' : '<div class="media-card-fallback">No preview</div>') +
                '</div>' +
                '<div class="media-card-body">' +
                '<strong>' + esc(asset.filename || asset.id) + '</strong>' +
                '<small>' + esc(formatBytes(asset.size)) + '</small>' +
                '<div class="media-card-actions">' +
                '<button type="button" class="secondary media-preview-btn">Preview</button>' +
                '<button type="button" class="secondary media-copy-btn">Copy URL</button>' +
                (onSelect ? '<button type="button" class="media-select-btn">Select</button>' : '') +
                '<button type="button" class="contrast media-delete-btn">Delete</button>' +
                '</div>' +
                '</div>' +
                '</article>';
        }
        container.innerHTML = html;

        var cards = container.querySelectorAll(".media-card");
        for (var j = 0; j < cards.length; j++) {
            (function (card) {
                var assetID = card.getAttribute("data-asset-id");
                var asset = findAssetByID(assets, assetID);
                card.querySelector(".media-preview-btn").addEventListener("click", function () {
                    openAssetPreview(asset, onSelect);
                });
                card.querySelector(".media-copy-btn").addEventListener("click", function () {
                    copyText(asset.url);
                    showMessage('Copied URL for ' + (asset.filename || asset.id) + '.', false);
                });
                if (onSelect) {
                    card.querySelector(".media-select-btn").addEventListener("click", function () {
                        onSelect(asset);
                    });
                }
                card.querySelector(".media-delete-btn").addEventListener("click", function () {
                    if (!window.confirm('Delete ' + (asset.filename || asset.id) + '?')) {
                        return;
                    }
                    api('/admin/media/' + encodeURIComponent(asset.id), { method: 'DELETE' }).then(function (body) {
                        if (body && body.error) {
                            showMessage(body.error.message || 'Delete failed.', true);
                            return;
                        }
                        showMessage('Deleted ' + (asset.filename || asset.id) + '.', false);
                        reload();
                    }).catch(function (err) {
                        showMessage(extractErrorMessage(err, 'Delete failed.'), true);
                    });
                });
            })(cards[j]);
        }
    }

    function openMediaPicker(onSelect) {
        var overlay = document.createElement("div");
        overlay.className = "media-modal-overlay";
        overlay.innerHTML = '' +
            '<div class="media-modal">' +
            '<header><h3>Select Image</h3><button type="button" id="close-media-modal" class="secondary">Close</button></header>' +
            '<div id="media-picker-msg"></div>' +
            '<div id="media-picker-grid" class="media-grid"></div>' +
            '</div>';
        document.body.appendChild(overlay);

        function close() {
            document.body.removeChild(overlay);
        }

        document.getElementById("close-media-modal").addEventListener("click", close);
        overlay.addEventListener("click", function (e) {
            if (e.target === overlay) {
                close();
            }
        });

        var msg = document.getElementById("media-picker-msg");
        var grid = document.getElementById("media-picker-grid");
        api("/admin/media?offset=0&limit=100").then(function (body) {
            var assets = normalizeAssets(body && body.data && body.data.assets ? body.data.assets : []);
            renderMediaAssets(grid, assets, function (asset) {
                onSelect(asset);
                close();
            }, function () {
                api("/admin/media?offset=0&limit=100").then(function (refreshBody) {
                    renderMediaAssets(grid, normalizeAssets(refreshBody && refreshBody.data && refreshBody.data.assets ? refreshBody.data.assets : []), function (asset) {
                        onSelect(asset);
                        close();
                    }, function () {}, function (text, isError) {
                        msg.innerHTML = text ? '<p' + (isError ? ' role="alert"' : '') + '>' + esc(text) + '</p>' : '';
                    });
                });
            }, function (text, isError) {
                msg.innerHTML = text ? '<p' + (isError ? ' role="alert"' : '') + '>' + esc(text) + '</p>' : '';
            });
        }).catch(function () {
            grid.innerHTML = '<p role="alert">Failed to load media library.</p>';
        });
    }

    function openAssetPreview(asset, onSelect) {
        var overlay = document.createElement("div");
        overlay.className = "media-modal-overlay";
        var previewURL = asset.thumbnails.medium || asset.thumbnails.small || asset.url;
        overlay.innerHTML = '' +
            '<div class="media-modal media-preview-modal">' +
            '<header><h3>' + esc(asset.filename || asset.id) + '</h3><button type="button" id="close-asset-preview" class="secondary">Close</button></header>' +
            '<div class="media-preview-body">' +
            (previewURL ? '<img src="' + esc(previewURL) + '" alt="' + esc(asset.filename || asset.id) + '">' : '<p>No preview available.</p>') +
            '<p><strong>URL:</strong> ' + esc(asset.url || '') + '</p>' +
            '<div class="media-card-actions">' +
            '<button type="button" id="copy-asset-url" class="secondary">Copy URL</button>' +
            (onSelect ? '<button type="button" id="select-preview-asset">Select</button>' : '') +
            '</div>' +
            '</div>' +
            '</div>';
        document.body.appendChild(overlay);

        function close() {
            document.body.removeChild(overlay);
        }

        document.getElementById("close-asset-preview").addEventListener("click", close);
        document.getElementById("copy-asset-url").addEventListener("click", function () {
            copyText(asset.url);
        });
        if (onSelect) {
            document.getElementById("select-preview-asset").addEventListener("click", function () {
                onSelect(asset);
                close();
            });
        }
        overlay.addEventListener("click", function (e) {
            if (e.target === overlay) {
                close();
            }
        });
    }

    function findAssetByID(assets, assetID) {
        for (var i = 0; i < assets.length; i++) {
            if (assets[i].id === assetID) {
                return assets[i];
            }
        }
        return null;
    }

    function copyText(text) {
        if (navigator.clipboard && navigator.clipboard.writeText) {
            navigator.clipboard.writeText(text || "");
        }
    }

    function formatBytes(size) {
        var value = Number(size || 0);
        if (value < 1024) {
            return value + ' B';
        }
        if (value < 1024 * 1024) {
            return (value / 1024).toFixed(1) + ' KB';
        }
        return (value / (1024 * 1024)).toFixed(1) + ' MB';
    }

    function extractErrorMessage(err, fallback) {
        if (err && err.error && err.error.message) {
            return err.error.message;
        }
        if (err && err.message) {
            return err.message;
        }
        return fallback;
    }

    function renderOrderDetail(container, orderID) {
        container.innerHTML =
            '<h2>Order Detail</h2>' +
            '<p><a href="/admin/orders" data-link>Back to orders</a></p>' +
            '<div id="order-detail-msg"></div>' +
            '<div id="order-detail-body">Loading…</div>';

        var msg = document.getElementById("order-detail-msg");
        var bodyBox = document.getElementById("order-detail-body");

        function load() {
            api("/admin/orders/" + encodeURIComponent(orderID)).then(function (res) {
                var order = normalizeOrder(res && res.data && res.data.order);
                if (!order) {
                    bodyBox.innerHTML = '<p role="alert">Order not found.</p>';
                    return;
                }

                var next = getNextOrderStatuses(order.status);
                var statusForm = '<p>No further status transitions available.</p>';
                if (next.length > 0) {
                    statusForm = '<form id="order-status-form">' +
                        '<label>Change Status<select name="status">';
                    for (var i = 0; i < next.length; i++) {
                        statusForm += '<option value="' + esc(next[i]) + '">' + esc(next[i]) + '</option>';
                    }
                    statusForm += '</select></label> <button type="submit">Update</button></form>';
                }

                var items = order.items || [];
                var itemsHtml = '<table><thead><tr><th>Product</th><th>SKU</th><th>Qty</th><th>Price</th><th>Line Total</th></tr></thead><tbody>';
                if (items.length === 0) {
                    itemsHtml += '<tr><td colspan="5">No items.</td></tr>';
                } else {
                    for (var j = 0; j < items.length; j++) {
                        var it = items[j];
                        var qty = Number(it.quantity || 0);
                        var unit = Number(it.unit_price || 0);
                        itemsHtml += '<tr>' +
                            '<td>' + esc(it.name || '') + '</td>' +
                            '<td>' + esc(it.sku || '') + '</td>' +
                            '<td>' + esc(String(qty)) + '</td>' +
                            '<td>' + formatMoney(unit, it.currency || order.currency) + '</td>' +
                            '<td>' + formatMoney(unit * qty, it.currency || order.currency) + '</td>' +
                            '</tr>';
                    }
                }
                itemsHtml += '</tbody></table>';

                bodyBox.innerHTML =
                    '<article>' +
                    '<p><strong>Order ID:</strong> ' + esc(order.id) + '</p>' +
                    '<p><strong>Status:</strong> <span class="badge badge-' + esc(order.status) + '">' + esc(order.status) + '</span></p>' +
                    statusForm +
                    '<p><strong>Customer:</strong> ' + esc(order.customer_id || '') + '</p>' +
                    '<p><strong>Date:</strong> ' + esc(order.created_at || '') + '</p>' +
                    '<h3>Items</h3>' + itemsHtml +
                    '<p><strong>Total:</strong> ' + formatMoney(Number(order.total_amount || 0), order.currency) + '</p>' +
                    '<h3>Shipping</h3>' +
                    '<p>Shipping details are not available in the current order payload.</p>' +
                    '<h3>Payment</h3>' +
                    '<p>Status: ' + esc(derivePaymentStatus(order.status)) + '</p>' +
                    '</article>';

                var form = document.getElementById("order-status-form");
                if (form) {
                    form.addEventListener("submit", function (e) {
                        e.preventDefault();
                        var nextStatus = form.elements.status.value;
                        api("/admin/orders/" + encodeURIComponent(order.id), {
                            method: "PUT",
                            body: JSON.stringify({ status: nextStatus })
                        }).then(function (updateResp) {
                            if (updateResp && updateResp.error) {
                                msg.innerHTML = '<p role="alert">' + esc(updateResp.error.message || "Failed to update status") + '</p>';
                                return;
                            }
                            msg.innerHTML = '<p>Status updated.</p>';
                            load();
                        }).catch(function () {
                            msg.innerHTML = '<p role="alert">Failed to update status.</p>';
                        });
                    });
                }
            }).catch(function () {
                bodyBox.innerHTML = '<p role="alert">Failed to load order.</p>';
            });
        }

        load();
    }

    function getNextOrderStatuses(current) {
        if (current === "pending") {
            return ["confirmed", "failed", "cancelled"];
        }
        if (current === "confirmed") {
            return ["paid", "cancelled"];
        }
        return [];
    }

    function derivePaymentStatus(orderStatus) {
        if (orderStatus === "paid") {
            return "paid";
        }
        if (orderStatus === "failed") {
            return "failed";
        }
        if (orderStatus === "cancelled") {
            return "cancelled";
        }
        return "pending";
    }

    function setStat(id, value) {
        var el = document.getElementById(id);
        if (el) el.querySelector("p").textContent = value;
    }

    function formatMoney(amount, currency) {
        var val = (amount / 100).toFixed(2);
        return currency ? currency + " " + val : val;
    }

    function esc(str) {
        if (!str) return "";
        var d = document.createElement("div");
        d.appendChild(document.createTextNode(str));
        return d.innerHTML;
    }

    function renderPlaceholder(name) {
        return function (container) {
            container.innerHTML = "<h2>" + name + "</h2><p>This section will be available in a future update.</p>";
        };
    }

    // --- Logout ---

    function handleLogout(e) {
        e.preventDefault();
        var token = getToken();
        clearToken();
        if (token) {
            fetch(API_BASE + "/auth/logout", {
                method: "POST",
                headers: {
                    "Content-Type": "application/json",
                    "Authorization": "Bearer " + token
                }
            }).catch(function () { /* best effort */ });
        }
        navigateTo("/admin");
    }

    // --- Init ---

    function init() {
        // Intercept sidebar link clicks for client-side navigation.
        document.addEventListener("click", function (e) {
            var link = e.target.closest("a[data-link]");
            if (link) {
                e.preventDefault();
                navigateTo(link.getAttribute("href"));
            }
        });

        var logoutBtn = document.getElementById("admin-logout");
        if (logoutBtn) {
            logoutBtn.addEventListener("click", handleLogout);
        }

        window.addEventListener("popstate", handleRoute);
        handleRoute();
    }

    if (document.readyState === "loading") {
        document.addEventListener("DOMContentLoaded", init);
    } else {
        init();
    }
})();
