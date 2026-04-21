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

    function api(url, options) {
        options = options || {};
        var headers = options.headers || {};
        headers["Content-Type"] = "application/json";
        var token = getToken();
        if (token) {
            headers["Authorization"] = "Bearer " + token;
        }
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

    // --- Routing ---

    var routes = {
        "/admin": { title: "Login", render: renderLogin, auth: false },
        "/admin/dashboard": { title: "Dashboard", render: renderDashboard, auth: true },
        "/admin/products": { title: "Products", render: renderProductsGrid, auth: true },
        "/admin/orders": { title: "Orders", render: renderOrdersGrid, auth: true },
        "/admin/media": { title: "Media", render: renderPlaceholder("Media"), auth: true },
        "/admin/settings": { title: "Settings", render: renderPlaceholder("Settings"), auth: true }
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
            html += '<button type="submit">' + (productID ? 'Save Product' : 'Create Product') + '</button>';
            form.innerHTML = html;

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
        if (Object.keys(payload.attributes).length === 0) {
            delete payload.attributes;
        }
        return payload;
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
