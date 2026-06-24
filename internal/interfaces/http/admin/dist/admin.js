// admin.js — Admin SPA scaffold (vanilla JS, no build step)
(function () {
    "use strict";

    var TOKEN_KEY = "shopanda_admin_token";
    var ADMIN_SCOPE_KEY = "shopanda_admin_scope";
    var LOGIN_MESSAGE_KEY = "shopanda_admin_login_message";
    var API_BASE = "/api/v1";
    var ADMIN_STORE_HEADER = "X-Admin-Store-ID";
    var ADMIN_LANGUAGE_HEADER = "X-Admin-Language";
    var ADMIN_CURRENCY_HEADER = "X-Admin-Currency";

    var currentUser = null;
    var adminScopeStores = [];
    var adminScope = loadAdminScope();

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

    function setLoginMessage(message) {
        try {
            sessionStorage.setItem(LOGIN_MESSAGE_KEY, message || "");
        } catch (err) {
            // best effort
        }
    }

    function popLoginMessage() {
        try {
            var message = sessionStorage.getItem(LOGIN_MESSAGE_KEY) || "";
            sessionStorage.removeItem(LOGIN_MESSAGE_KEY);
            return message;
        } catch (err) {
            return "";
        }
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
        if (adminScope.store_id) {
            out[ADMIN_STORE_HEADER] = adminScope.store_id;
        }
        if (adminScope.language) {
            out[ADMIN_LANGUAGE_HEADER] = adminScope.language;
        }
        if (adminScope.currency) {
            out[ADMIN_CURRENCY_HEADER] = adminScope.currency;
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
                setLoginMessage("Your session expired. Sign in again to continue.");
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
            var headers = buildHeaders({});
            if (headers.Authorization) {
                xhr.setRequestHeader("Authorization", headers.Authorization);
            }
            if (headers[ADMIN_STORE_HEADER]) {
                xhr.setRequestHeader(ADMIN_STORE_HEADER, headers[ADMIN_STORE_HEADER]);
            }
            if (headers[ADMIN_LANGUAGE_HEADER]) {
                xhr.setRequestHeader(ADMIN_LANGUAGE_HEADER, headers[ADMIN_LANGUAGE_HEADER]);
            }
            if (headers[ADMIN_CURRENCY_HEADER]) {
                xhr.setRequestHeader(ADMIN_CURRENCY_HEADER, headers[ADMIN_CURRENCY_HEADER]);
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
                    setLoginMessage("Your session expired. Sign in again to continue.");
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
        // Sales
        "/admin/orders": { title: "Orders", render: renderOrdersGrid, auth: true },
        "/admin/sales/returns": { title: "Returns", render: renderPlaceholder("Returns"), auth: true },
        "/admin/sales/transactions": { title: "Transactions", render: renderPlaceholder("Transactions"), auth: true },
        // Catalog
        "/admin/products": { title: "Products", render: renderProductsGrid, auth: true },
        "/admin/catalog/categories": { title: "Categories", render: renderCategoriesPage, auth: true },
        "/admin/catalog/attributes": { title: "Attributes", render: renderAttributesGrid, auth: true },
        // Customers
        "/admin/customers": { title: "Customers", render: renderCustomersGrid, auth: true },
        "/admin/customers/groups": { title: "Groups", render: renderPlaceholder("Groups"), auth: true },
        // Marketing
        "/admin/marketing/promotions": { title: "Promotions", render: renderPromotionsGrid, auth: true },
        "/admin/marketing/coupons": { title: "Coupons", render: renderCouponsGrid, auth: true },
        // Content
        "/admin/content/pages": { title: "Pages", render: renderPagesGrid, auth: true },
        "/admin/content/navigation": { title: "Navigation", render: renderPlaceholder("Navigation"), auth: true },
        "/admin/content/blocks": { title: "Blocks", render: renderPlaceholder("Blocks"), auth: true },
        "/admin/media": { title: "Media", render: renderMediaLibrary, auth: true },
        // Operations
        "/admin/operations/inventory": { title: "Inventory", render: renderInventoryGrid, auth: true },
        "/admin/operations/shipping": { title: "Shipping", render: renderShippingSettingsPage, auth: true },
        "/admin/operations/payments": { title: "Payments", render: renderPaymentSettingsPage, auth: true },
        // Settings
        "/admin/settings": { title: "Settings", render: renderSettingsPage, auth: true },
        "/admin/settings/localization": { title: "Localization", render: renderLocalizationSettingsPage, auth: true },
        "/admin/settings/users": { title: "Users & Roles", render: renderUsersRolesPage, auth: true },
        "/admin/settings/audit": { title: "Audit Log", render: renderAuditLogPage, auth: true },
        // Store Management
        "/admin/store": { title: "Stores", render: renderStoresGrid, auth: true },
        "/admin/store/domains": { title: "Domains", render: renderStoreDomainsPage, auth: true },
        "/admin/store/languages": { title: "Languages", render: renderStoreLanguagesPage, auth: true },
        "/admin/store/currencies": { title: "Currencies", render: renderStoreCurrenciesPage, auth: true },
        // Integrations
        "/admin/integrations": { title: "Integrations", render: renderIntegrationsPage, auth: true },
        // Account (accessible from header user-info link)
        "/admin/account": { title: "Account", render: renderAdminAccount, auth: true }
    };

    function resolveRoute(path) {
        if (routes[path]) {
            return routes[path];
        }
        if (path === "/admin/products/new") {
            return { title: "New Product", render: renderProductCreate, auth: true };
        }
        if (path === "/admin/categories/new") {
            return { title: "New Category", render: renderCategoryCreate, auth: true };
        }
        if (path === "/admin/content/pages/new") {
            return { title: "New Page", render: renderPageCreate, auth: true };
        }
        if (path === "/admin/marketing/coupons/new") {
            return { title: "New Coupon", render: renderCouponCreate, auth: true };
        }
        if (path === "/admin/marketing/promotions/new") {
            return { title: "New Promotion", render: renderPromotionCreate, auth: true };
        }
        if (path === "/admin/catalog/attributes/new") {
            return { title: "New Attribute", render: renderAttributeCreate, auth: true };
        }
        if (path === "/admin/catalog/attribute-groups/new") {
            return { title: "New Attribute Group", render: renderAttributeGroupCreate, auth: true };
        }
        var attributeMatch = path.match(/^\/admin\/catalog\/attributes\/([^/]+)$/);
        if (attributeMatch) {
            var attributeCode = decodeURIComponent(attributeMatch[1]);
            return {
                title: "Edit Attribute",
                auth: true,
                render: function (container) { renderAttributeEdit(container, attributeCode); }
            };
        }
        var attributeGroupMatch = path.match(/^\/admin\/catalog\/attribute-groups\/([^/]+)$/);
        if (attributeGroupMatch) {
            var groupCode = decodeURIComponent(attributeGroupMatch[1]);
            return {
                title: "Edit Attribute Group",
                auth: true,
                render: function (container) { renderAttributeGroupEdit(container, groupCode); }
            };
        }
        var promotionMatch = path.match(/^\/admin\/marketing\/promotions\/([^/]+)$/);
        if (promotionMatch) {
            var promotionID = decodeURIComponent(promotionMatch[1]);
            return {
                title: "Edit Promotion",
                auth: true,
                render: function (container) { renderPromotionEdit(container, promotionID); }
            };
        }
        var couponMatch = path.match(/^\/admin\/marketing\/coupons\/([^/]+)$/);
        if (couponMatch) {
            var couponID = decodeURIComponent(couponMatch[1]);
            return {
                title: "Edit Coupon",
                auth: true,
                render: function (container) { renderCouponEdit(container, couponID); }
            };
        }
        var pageMatch = path.match(/^\/admin\/content\/pages\/([^/]+)$/);
        if (pageMatch) {
            var pageID = decodeURIComponent(pageMatch[1]);
            return {
                title: "Edit Page",
                auth: true,
                render: function (container) { renderPageEdit(container, pageID); }
            };
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
        var categoryMatch = path.match(/^\/admin\/categories\/([^/]+)$/);
        if (categoryMatch) {
            var categoryID = decodeURIComponent(categoryMatch[1]);
            return {
                title: "Edit Category",
                auth: true,
                render: function (container) { renderCategoryEdit(container, categoryID); }
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
        var customerMatch = path.match(/^\/admin\/customers\/([^/]+)$/);
        if (customerMatch) {
            var customerID = decodeURIComponent(customerMatch[1]);
            return {
                title: "Customer Detail",
                auth: true,
                render: function (container) { renderCustomerDetail(container, customerID); }
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
            var gridResponse = results[0] || {};
            var productsResponse = results[1] || {};
            if ((gridResponse.error && gridResponse.error.code === "forbidden") ||
                (productsResponse.error && productsResponse.error.code === "forbidden")) {
                gridBox.innerHTML = '<p role="alert">Your account does not have products access.</p>';
                return;
            }
            var grid = gridResponse.data && gridResponse.data.grid;
            var productsRaw = productsResponse.data && productsResponse.data.products;
            if (!grid) {
                gridBox.innerHTML = '<p role="alert">' + esc(extractErrorMessage(gridResponse, 'Failed to load product grid.')) + '</p>';
                return;
            }
            if (!Array.isArray(productsRaw)) {
                gridBox.innerHTML = '<p role="alert">' + esc(extractErrorMessage(productsResponse, 'Failed to load products.')) + '</p>';
                return;
            }
            var products = normalizeProducts(productsRaw);
            var html = '<div style="margin-bottom:1rem"><button id="new-product-btn">New Product</button></div>';
            html += '<table class="admin-table"><thead><tr>';
            for (var i = 0; i < grid.columns.length; i++) {
                html += '<th scope="col">' + esc(grid.columns[i].label || grid.columns[i].name) + '</th>';
            }
            html += '<th scope="col">Action</th></tr></thead><tbody>';
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
        }).catch(function (err) {
            gridBox.innerHTML = '<p role="alert">' + esc(extractErrorMessage(err, 'Failed to load products.')) + '</p>';
        });
    }

    function renderCategoriesPage(container) {
        container.innerHTML = '' +
            '<h2>Categories</h2>' +
            '<div id="categories-msg"></div>' +
            '<div style="margin-bottom:1rem"><button id="new-category-btn">New Category</button></div>' +
            '<div id="categories-tree"></div>';

        var msg = document.getElementById('categories-msg');
        var tree = document.getElementById('categories-tree');

        function setMessage(text, isError) {
            msg.innerHTML = text ? '<p' + (isError ? ' role="alert"' : ' role="status" aria-live="polite"') + '>' + esc(text) + '</p>' : '';
        }

        function loadCategories() {
            tree.innerHTML = '<p>Loading…</p>';
            api('/admin/categories').then(function (body) {
                if (body && body.error && body.error.code === 'forbidden') {
                    tree.innerHTML = '<p role="alert">Your account does not have categories access.</p>';
                    return;
                }

                var categories = normalizeCategoryTree(body && body.data && body.data.categories);
                if (!Array.isArray(categories)) {
                    tree.innerHTML = '<p role="alert">' + esc(extractErrorMessage(body, 'Failed to load categories.')) + '</p>';
                    return;
                }

                if (categories.length === 0) {
                    tree.innerHTML = '<p>No categories found.</p>';
                    return;
                }

                tree.innerHTML = renderCategoryTreeNodes(categories);
                bindCategoryTreeActions(tree, categories, setMessage, loadCategories);
            }).catch(function (err) {
                tree.innerHTML = '<p role="alert">' + esc(extractErrorMessage(err, 'Failed to load categories.')) + '</p>';
            });
        }

        document.getElementById('new-category-btn').addEventListener('click', function () {
            navigateTo('/admin/categories/new');
        });

        loadCategories();
    }

    function renderCategoryTreeNodes(nodes) {
        if (!Array.isArray(nodes) || nodes.length === 0) {
            return '';
        }

        var html = '<ul class="admin-tree-list">';
        for (var i = 0; i < nodes.length; i++) {
            var node = nodes[i] || {};
            var children = Array.isArray(node.children) ? node.children : [];
            var categoryLabel = esc((node.name || node.slug || node.id || 'Category'));
            var orderingControls = '';
            if (i > 0) {
                orderingControls += ' <button type="button" aria-label="Move category ' + categoryLabel + ' up" data-category-move="up" data-category-id="' + esc(String(node.id || '')) + '">Move up</button>';
            }
            if (i < nodes.length - 1) {
                orderingControls += ' <button type="button" aria-label="Move category ' + categoryLabel + ' down" data-category-move="down" data-category-id="' + esc(String(node.id || '')) + '">Move down</button>';
            }
            html += '<li>' +
                '<div>' +
                '<strong>' + esc(node.name || node.slug || node.id || '') + '</strong>' +
                ' <span class="settings-scope-note">/' + esc(node.slug || '') + '</span>' +
                ' <span class="settings-scope-note">position ' + esc(String(node.position == null ? 0 : node.position)) + '</span>' +
                orderingControls +
                ' <a href="/admin/categories/' + encodeURIComponent(String(node.id || '')) + '" data-link>Edit</a>' +
                ' <button type="button" aria-label="Delete category ' + categoryLabel + '" data-category-delete="' + esc(String(node.id || '')) + '" data-category-name="' + esc(String(node.name || node.slug || node.id || '')) + '">Delete</button>' +
                '</div>' +
                renderCategoryTreeNodes(children) +
                '</li>';
        }
        html += '</ul>';
        return html;
    }

    function bindCategoryTreeActions(container, categories, setMessage, reload) {
        var deleteButtons = container.querySelectorAll('[data-category-delete]');
        for (var i = 0; i < deleteButtons.length; i++) {
            deleteButtons[i].addEventListener('click', function () {
                var categoryID = this.getAttribute('data-category-delete');
                var categoryName = this.getAttribute('data-category-name') || categoryID;
                if (!window.confirm('Delete ' + categoryName + '?')) {
                    return;
                }
                api('/admin/categories/' + encodeURIComponent(categoryID), { method: 'DELETE' }).then(function (body) {
                    if (body && body.error) {
                        setMessage(body.error.message || 'Failed to delete category.', true);
                        return;
                    }
                    setMessage('Category deleted.', false);
                    reload();
                }).catch(function (err) {
                    setMessage(extractErrorMessage(err, 'Failed to delete category.'), true);
                });
            });
        }

        var moveButtons = container.querySelectorAll('[data-category-move]');
        for (var j = 0; j < moveButtons.length; j++) {
            moveButtons[j].addEventListener('click', function () {
                var categoryID = this.getAttribute('data-category-id');
                var direction = this.getAttribute('data-category-move');
                moveCategoryOrdering(categories, categoryID, direction, setMessage, reload);
            });
        }
    }

    function moveCategoryOrdering(categories, categoryID, direction, setMessage, reload) {
        var match = findCategorySiblings(categories, categoryID);
        if (!match || !Array.isArray(match.siblings)) {
            setMessage('Failed to save category order.', true);
            return;
        }

        var fromIndex = match.index;
        var toIndex = direction === 'up' ? fromIndex - 1 : fromIndex + 1;
        if (toIndex < 0 || toIndex >= match.siblings.length) {
            return;
        }

        var reordered = match.siblings.slice();
        var moved = reordered.splice(fromIndex, 1)[0];
        reordered.splice(toIndex, 0, moved);

        var requests = [];
        for (var i = 0; i < reordered.length; i++) {
            var sibling = reordered[i] || {};
            var nextPosition = i;
            if (Number(sibling.position) === nextPosition) {
                continue;
            }
            requests.push(api('/admin/categories/' + encodeURIComponent(String(sibling.id || '')), {
                method: 'PUT',
                body: JSON.stringify({ position: nextPosition })
            }));
        }

        if (requests.length === 0) {
            return;
        }

        Promise.all(requests).then(function (responses) {
            for (var k = 0; k < responses.length; k++) {
                if (responses[k] && responses[k].error) {
                    setMessage(responses[k].error.message || 'Failed to save category order.', true);
                    return;
                }
            }
            setMessage('Category order saved.', false);
            reload();
        }).catch(function (err) {
            setMessage(extractErrorMessage(err, 'Failed to save category order.'), true);
        });
    }

    function findCategorySiblings(nodes, categoryID) {
        if (!Array.isArray(nodes)) {
            return null;
        }
        for (var i = 0; i < nodes.length; i++) {
            var node = nodes[i] || {};
            if (String(node.id || '') === String(categoryID || '')) {
                return { siblings: nodes, index: i };
            }
            var nested = findCategorySiblings(node.children, categoryID);
            if (nested) {
                return nested;
            }
        }
        return null;
    }

    function renderCategoryCreate(container) {
        renderCategoryForm(container, null);
    }

    function renderCategoryEdit(container, categoryID) {
        renderCategoryForm(container, categoryID);
    }

    function renderCategoryForm(container, categoryID) {
        var title = categoryID ? 'Edit Category' : 'New Category';
        container.innerHTML =
            '<h2>' + title + '</h2>' +
            '<p><a href="/admin/catalog/categories" data-link>Back to categories</a></p>' +
            '<div id="category-form-msg"></div>' +
            '<form id="category-form"><p>Loading…</p></form>' +
            '<section id="category-products-panel" style="display:none; margin-top:2rem;">' +
            '<h3>Assigned Products</h3>' +
            '<div id="category-products-msg"></div>' +
            '<div id="category-products-body"></div>' +
            '</section>';

        var msg = document.getElementById('category-form-msg');
        var form = document.getElementById('category-form');
        var requests = [api('/admin/categories')];
        if (categoryID) {
            requests.push(api('/admin/categories/' + encodeURIComponent(categoryID)));
        }

        Promise.all(requests).then(function (results) {
            var categoriesBody = results[0] || {};
            if (categoriesBody.error && categoriesBody.error.code === 'forbidden') {
                msg.innerHTML = '<p role="alert">Your account does not have categories access.</p>';
                form.innerHTML = '';
                return;
            }

            var categories = normalizeCategoryTree(categoriesBody.data && categoriesBody.data.categories);
            if (!Array.isArray(categories)) {
                msg.innerHTML = '<p role="alert">' + esc(extractErrorMessage(categoriesBody, 'Failed to load category form.')) + '</p>';
                form.innerHTML = '';
                return;
            }

            var category = null;
            if (categoryID) {
                var detailBody = results[1] || {};
                if (detailBody.error && detailBody.error.code === 'forbidden') {
                    msg.innerHTML = '<p role="alert">Your account does not have categories access.</p>';
                    form.innerHTML = '';
                    return;
                }
                category = normalizeCategory(detailBody.data && detailBody.data.category);
                if (!category) {
                    msg.innerHTML = '<p role="alert">Category not found.</p>';
                    form.innerHTML = '';
                    return;
                }
            }

            form.innerHTML = renderCategoryFormFields(categories, category);

            form.addEventListener('submit', function (e) {
                e.preventDefault();

                var meta;
                try {
                    meta = parseCategoryMeta(form.elements.meta.value);
                } catch (err) {
                    msg.innerHTML = '<p role="alert">' + esc(err.message || 'Category meta must be a JSON object.') + '</p>';
                    return;
                }

                var payload = {
                    name: form.elements.name.value,
                    slug: form.elements.slug.value,
                    parent_id: form.elements.parent_id.value,
                    position: Number(form.elements.position.value || 0),
                    meta: meta
                };
                var method = categoryID ? 'PUT' : 'POST';
                var url = categoryID ? '/admin/categories/' + encodeURIComponent(categoryID) : '/admin/categories';
                api(url, { method: method, body: JSON.stringify(payload) }).then(function (body) {
                    if (body && body.error) {
                        msg.innerHTML = '<p role="alert">' + esc(body.error.message || 'Save failed.') + '</p>';
                        return;
                    }
                    msg.innerHTML = '<p>Saved.</p>';
                    var savedCategory = normalizeCategory(body && body.data && body.data.category);
                    if (!categoryID && savedCategory && savedCategory.id) {
                        navigateTo('/admin/categories/' + encodeURIComponent(savedCategory.id));
                    }
                }).catch(function (err) {
                    msg.innerHTML = '<p role="alert">' + esc(extractErrorMessage(err, 'Save failed.')) + '</p>';
                });
            });

            if (categoryID) {
                setupCategoryProductAssignment(categoryID);
                var deleteBtn = document.getElementById('delete-category-btn');
                if (deleteBtn) {
                    deleteBtn.addEventListener('click', function () {
                        if (!window.confirm('Delete ' + (category.name || category.slug || category.id) + '?')) {
                            return;
                        }
                        api('/admin/categories/' + encodeURIComponent(categoryID), { method: 'DELETE' }).then(function (body) {
                            if (body && body.error) {
                                msg.innerHTML = '<p role="alert">' + esc(body.error.message || 'Failed to delete category.') + '</p>';
                                return;
                            }
                            navigateTo('/admin/catalog/categories');
                        }).catch(function (err) {
                            msg.innerHTML = '<p role="alert">' + esc(extractErrorMessage(err, 'Failed to delete category.')) + '</p>';
                        });
                    });
                }
            }
        }).catch(function (err) {
            msg.innerHTML = '<p role="alert">' + esc(extractErrorMessage(err, 'Failed to load category form.')) + '</p>';
            form.innerHTML = '';
        });
    }

    function setupCategoryProductAssignment(categoryID) {
        var panel = document.getElementById('category-products-panel');
        var msg = document.getElementById('category-products-msg');
        var body = document.getElementById('category-products-body');
        var pickerState = { offset: 0, limit: 50, search: '' };
        var assignedState = { offset: 0, limit: 20, search: '' };
        var assignedLookupLimit = 100;
        if (!panel || !msg || !body) {
            return;
        }
        panel.style.display = '';

        function setMessage(text, isError) {
            msg.innerHTML = text ? '<p' + (isError ? ' role="alert"' : ' role="status" aria-live="polite"') + '>' + esc(text) + '</p>' : '';
        }

        function loadAllAssignedProducts() {
            var all = [];

            function loadBatch(offset) {
                return api('/admin/categories/' + encodeURIComponent(categoryID) + '/products?offset=' + offset + '&limit=' + assignedLookupLimit).then(function (body) {
                    if (body && body.error) {
                        return Promise.reject(body);
                    }
                    var products = normalizeProducts(body && body.data && body.data.products);
                    if (!Array.isArray(products)) {
                        return Promise.reject(body);
                    }
                    for (var i = 0; i < products.length; i++) {
                        all.push(products[i]);
                    }
                    if (products.length < assignedLookupLimit) {
                        return all;
                    }
                    return loadBatch(offset + assignedLookupLimit);
                });
            }

            return loadBatch(0);
        }

        function loadAssignments() {
            body.innerHTML = '<p>Loading…</p>';
            Promise.all([
                api('/admin/categories/' + encodeURIComponent(categoryID) + '/products?offset=' + assignedState.offset + '&limit=' + assignedState.limit),
                loadAllAssignedProducts(),
                api('/admin/products?offset=' + pickerState.offset + '&limit=' + pickerState.limit)
            ]).then(function (results) {
                var assignedBody = results[0] || {};
                var allAssignedProducts = results[1] || [];
                var productsBody = results[2] || {};
                if (assignedBody.error && assignedBody.error.code === 'forbidden') {
                    body.innerHTML = '<p role="alert">Your account does not have categories access.</p>';
                    return;
                }
                if (productsBody.error && productsBody.error.code === 'forbidden') {
                    body.innerHTML = '<p role="alert">Your account does not have products access, so product assignment is unavailable.</p>';
                    return;
                }

                var assignedProducts = normalizeProducts(assignedBody.data && assignedBody.data.products);
                var allProducts = normalizeProducts(productsBody.data && productsBody.data.products);
                if (!Array.isArray(assignedProducts) || !Array.isArray(allAssignedProducts) || !Array.isArray(allProducts)) {
                    body.innerHTML = '<p role="alert">' + esc(extractErrorMessage(assignedBody.error ? assignedBody : productsBody, 'Failed to load assigned products.')) + '</p>';
                    return;
                }

                // Keep the user on their current assigned-products page when possible,
                // but step back one page if a mutation emptied the current page.
                if (assignedState.offset > 0 && assignedProducts.length === 0) {
                    assignedState.offset = Math.max(0, assignedState.offset - assignedState.limit);
                    loadAssignments();
                    return;
                }

                var assignedIDs = {};
                for (var i = 0; i < allAssignedProducts.length; i++) {
                    assignedIDs[String(allAssignedProducts[i].id || '')] = true;
                }

                var availableProducts = [];
                for (var j = 0; j < allProducts.length; j++) {
                    var candidate = allProducts[j] || {};
                    if (!assignedIDs[String(candidate.id || '')]) {
                        availableProducts.push(candidate);
                    }
                }

                renderCategoryProductAssignmentView(assignedProducts, availableProducts, allProducts.length);
            }).catch(function (err) {
                body.innerHTML = '<p role="alert">' + esc(extractErrorMessage(err, 'Failed to load assigned products.')) + '</p>';
            });
        }

        function renderCategoryProductAssignmentView(assignedProducts, availableProducts, loadedProductCount) {
            body.innerHTML = renderCategoryProductAssignmentBody(assignedProducts, availableProducts, pickerState, loadedProductCount, assignedState);
            bindCategoryProductAssignmentActions(categoryID, body, setMessage, loadAssignments, renderCategoryProductAssignmentView, assignedProducts, availableProducts, loadedProductCount, pickerState, assignedState);
        }

        loadAssignments();
    }

    function renderCategoryProductAssignmentBody(assignedProducts, availableProducts, pickerState, loadedProductCount, assignedState) {
        var filteredAssignedProducts = filterProductAssignmentOptions(assignedProducts, pickerState.search);
        var filteredAvailableProducts = filterProductAssignmentOptions(availableProducts, pickerState.search);
        var pageNumber = Math.floor(pickerState.offset / pickerState.limit) + 1;
        var hasPreviousPage = pickerState.offset > 0;
        var hasNextPage = loadedProductCount >= pickerState.limit;
        var assignedPageNumber = Math.floor((assignedState && assignedState.offset || 0) / (assignedState && assignedState.limit || 20)) + 1;
        var totalAssignedPages = Math.max(1, Math.ceil(assignedProducts.length / (assignedState && assignedState.limit || 20)));
        var hasAssignedPrev = (assignedState && assignedState.offset || 0) > 0;
        var hasAssignedNext = assignedProducts.length >= (assignedState && assignedState.limit || 20);
        var html = '<div style="margin-bottom:1rem">';
        html += '<p class="settings-scope-note">Product picker page ' + esc(String(pageNumber)) + '.</p>';
        html += '<label>Filter products<input type="search" id="category-product-search" placeholder="Search products" value="' + esc(pickerState.search || '') + '"></label>';
        if (availableProducts.length > 0 && filteredAvailableProducts.length > 0) {
            html += '<label>Assign product<select id="category-assignment-product">';
            for (var i = 0; i < filteredAvailableProducts.length; i++) {
                var product = filteredAvailableProducts[i] || {};
                html += '<option value="' + esc(String(product.id || '')) + '">' + esc((product.name || product.slug || product.id || '') + ' (' + (product.slug || product.id || '') + ')') + '</option>';
            }
            html += '</select></label> <button type="button" id="assign-category-product-btn">Assign Product</button>';
        } else if (availableProducts.length > 0) {
            html += '<p class="settings-scope-note">No available products match this filter.</p>';
        } else if (loadedProductCount > 0) {
            html += '<p class="settings-scope-note">All products on this page are already assigned to this category.</p>';
        } else {
            html += '<p class="settings-scope-note">No products found on this page.</p>';
        }
        html += '<div style="margin-top:0.75rem">' +
            '<button type="button" id="category-assignment-prev-page"' + (hasPreviousPage ? '' : ' disabled') + '>Previous Product Page</button> ' +
            '<button type="button" id="category-assignment-next-page"' + (hasNextPage ? '' : ' disabled') + '>Next Product Page</button>' +
            '</div>';
        html += '</div>';
        html += '<div style="margin-bottom:0.5rem">';
        html += '<p class="settings-scope-note">Assigned products page ' + esc(String(assignedPageNumber)) + ' of ' + esc(String(totalAssignedPages)) + '.</p>';
        html += '<button type="button" id="assigned-products-prev-page"' + (hasAssignedPrev ? '' : ' disabled') + '>Previous Assigned Page</button> ';
        html += '<button type="button" id="assigned-products-next-page"' + (hasAssignedNext ? '' : ' disabled') + '>Next Assigned Page</button>';
        html += '</div>';
        html += '<table class="admin-table"><thead><tr><th scope="col">Name</th><th scope="col">Slug</th><th scope="col">Status</th><th scope="col">Action</th></tr></thead><tbody>';
        if (assignedProducts.length === 0) {
            html += '<tr><td colspan="4">No products assigned.</td></tr>';
        } else if (filteredAssignedProducts.length === 0) {
            html += '<tr><td colspan="4">No assigned products match this filter.</td></tr>';
        } else {
            for (var j = 0; j < filteredAssignedProducts.length; j++) {
                var assigned = filteredAssignedProducts[j] || {};
                var productLabel = esc((assigned.name || assigned.slug || assigned.id || 'Product'));
                html += '<tr>' +
                    '<td>' + esc(assigned.name || '') + '</td>' +
                    '<td>' + esc(assigned.slug || '') + '</td>' +
                    '<td>' + esc(assigned.status || '') + '</td>' +
                    '<td><button type="button" aria-label="Remove product ' + productLabel + '" data-category-product-remove="' + esc(String(assigned.id || '')) + '">Remove</button></td>' +
                    '</tr>';
            }
        }
        html += '</tbody></table>';
        return html;
    }

    function bindCategoryProductAssignmentActions(categoryID, container, setMessage, reload, rerender, assignedProducts, availableProducts, loadedProductCount, pickerState, assignedState) {
        var searchInput = document.getElementById('category-product-search');
        if (searchInput) {
            searchInput.addEventListener('input', function () {
                pickerState.search = this.value || '';
                pickerState.offset = 0;
                assignedState.offset = 0;
                reload();
            });
        }
        var assignedPrevBtn = document.getElementById('assigned-products-prev-page');
        if (assignedPrevBtn) {
            assignedPrevBtn.addEventListener('click', function () {
                if (assignedState.offset <= 0) return;
                assignedState.offset = Math.max(0, assignedState.offset - assignedState.limit);
                reload();
            });
        }
        var assignedNextBtn = document.getElementById('assigned-products-next-page');
        if (assignedNextBtn) {
            assignedNextBtn.addEventListener('click', function () {
                assignedState.offset += assignedState.limit;
                reload();
            });
        }

        var assignBtn = document.getElementById('assign-category-product-btn');
        if (assignBtn) {
            assignBtn.addEventListener('click', function () {
                var select = document.getElementById('category-assignment-product');
                var productID = select && select.value ? select.value : '';
                if (!productID) {
                    return;
                }
                api('/admin/categories/' + encodeURIComponent(categoryID) + '/products/' + encodeURIComponent(productID), { method: 'POST' }).then(function (body) {
                    if (body && body.error) {
                        setMessage(body.error.message || 'Failed to assign product.', true);
                        return;
                    }
                    setMessage('Product assigned.', false);
                    reload();
                }).catch(function (err) {
                    setMessage(extractErrorMessage(err, 'Failed to assign product.'), true);
                });
            });
        }

        var prevBtn = document.getElementById('category-assignment-prev-page');
        if (prevBtn) {
            prevBtn.addEventListener('click', function () {
                if (pickerState.offset <= 0) {
                    return;
                }
                pickerState.offset = Math.max(0, pickerState.offset - pickerState.limit);
                reload();
            });
        }

        var nextBtn = document.getElementById('category-assignment-next-page');
        if (nextBtn) {
            nextBtn.addEventListener('click', function () {
                pickerState.offset += pickerState.limit;
                reload();
            });
        }

        var removeButtons = container.querySelectorAll('[data-category-product-remove]');
        for (var i = 0; i < removeButtons.length; i++) {
            removeButtons[i].addEventListener('click', function () {
                var productID = this.getAttribute('data-category-product-remove');
                api('/admin/categories/' + encodeURIComponent(categoryID) + '/products/' + encodeURIComponent(productID), { method: 'DELETE' }).then(function (body) {
                    if (body && body.error) {
                        setMessage(body.error.message || 'Failed to remove product.', true);
                        return;
                    }
                    setMessage('Product removed from category.', false);
                    reload();
                }).catch(function (err) {
                    setMessage(extractErrorMessage(err, 'Failed to remove product.'), true);
                });
            });
        }
    }

    function filterProductAssignmentOptions(products, searchTerm) {
        if (!Array.isArray(products)) {
            return [];
        }
        var query = String(searchTerm || '').trim().toLowerCase();
        if (!query) {
            return products.slice();
        }
        var out = [];
        for (var i = 0; i < products.length; i++) {
            var product = products[i] || {};
            var text = ((product.name || '') + ' ' + (product.slug || '') + ' ' + (product.id || '')).toLowerCase();
            if (text.indexOf(query) !== -1) {
                out.push(product);
            }
        }
        return out;
    }

    function renderCategoryFormFields(categories, category) {
        var parentID = category && category.parent_id ? category.parent_id : '';
        var options = flattenCategoryOptions(categories, 0, []);
        var html = '' +
            '<label>Name<input name="name" required value="' + esc(category && category.name ? category.name : '') + '"></label>' +
            '<label>Slug<input name="slug" required value="' + esc(category && category.slug ? category.slug : '') + '"></label>' +
            '<label>Parent<select name="parent_id"><option value="">Top level</option>' + renderCategoryParentOptions(options, parentID, category && category.id) + '</select></label>' +
            '<label>Position<input type="number" name="position" value="' + esc(String(category && category.position != null ? category.position : 0)) + '"></label>' +
            '<label>Meta (JSON)<textarea name="meta">' + esc(formatCategoryMeta(category && category.meta ? category.meta : {})) + '</textarea></label>' +
            '<button type="submit">' + (category ? 'Save Category' : 'Create Category') + '</button>';
        if (category) {
            html += ' <button type="button" id="delete-category-btn" class="contrast">Delete Category</button>';
        }
        return html;
    }

    function renderCategoryParentOptions(options, selectedID, excludeID) {
        var html = '';
        for (var i = 0; i < options.length; i++) {
            var option = options[i];
            if (excludeID && option.id === excludeID) {
                continue;
            }
            var selected = option.id === selectedID ? ' selected' : '';
            html += '<option value="' + esc(option.id) + '"' + selected + '>' + esc(option.label) + '</option>';
        }
        return html;
    }

    function flattenCategoryOptions(nodes, depth, out) {
        out = out || [];
        if (!Array.isArray(nodes)) {
            return out;
        }
        for (var i = 0; i < nodes.length; i++) {
            var node = nodes[i] || {};
            var prefix = '';
            for (var j = 0; j < depth; j++) {
                prefix += '-- ';
            }
            out.push({
                id: node.id || '',
                slug: node.slug || '',
                label: prefix + (node.name || node.slug || node.id || '')
            });
            flattenCategoryOptions(node.children, depth + 1, out);
        }
        return out;
    }

    function normalizeCategory(raw) {
        if (!raw) {
            return null;
        }
        return {
            id: pick(raw, 'id', 'ID'),
            parent_id: pick(raw, 'parent_id', 'ParentID'),
            name: pick(raw, 'name', 'Name'),
            slug: pick(raw, 'slug', 'Slug'),
            position: pick(raw, 'position', 'Position'),
            meta: pick(raw, 'meta', 'Meta') || {},
            created_at: pick(raw, 'created_at', 'CreatedAt'),
            updated_at: pick(raw, 'updated_at', 'UpdatedAt')
        };
    }

    function normalizeCategoryTree(nodes) {
        if (!Array.isArray(nodes)) {
            return null;
        }
        var out = [];
        for (var i = 0; i < nodes.length; i++) {
            var node = normalizeCategory(nodes[i]);
            if (!node) {
                continue;
            }
            node.children = normalizeCategoryTree((nodes[i] || {}).children) || [];
            out.push(node);
        }
        return out;
    }

    function formatCategoryMeta(meta) {
        try {
            return JSON.stringify(meta || {}, null, 2);
        } catch (err) {
            return '{}';
        }
    }

    function parseCategoryMeta(raw) {
        var value = String(raw || '').trim();
        if (!value) {
            return {};
        }
        var parsed = JSON.parse(value);
        if (!parsed || Array.isArray(parsed) || typeof parsed !== 'object') {
            throw new Error('Category meta must be a JSON object.');
        }
        return parsed;
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
            '<div id="product-scope-banner" class="settings-scope-banner"></div>' +
            '<div id="product-form-msg"></div>' +
            '<form id="product-form"></form>' +
            '<section id="product-category-panel" style="display:none; margin-top:2rem;">' +
            '<h3>Assigned Categories</h3>' +
            '<div id="product-category-msg"></div>' +
            '<div id="product-category-body"></div>' +
            '</section>' +
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
        var activeScope = {
            storeID: adminScope.store_id || '',
            language: adminScope.language || '',
            currency: adminScope.currency || ''
        };
        renderProductScopeBanner(activeScope);

        var requests = [api("/admin/forms/product.form")];
        if (productID) {
            requests.push(api("/admin/products/" + encodeURIComponent(productID)));
            if (activeScope.language) {
                requests.push(api("/admin/products/" + encodeURIComponent(productID) + "/translations"));
            }
        }

        Promise.all(requests).then(function (results) {
            var schema = results[0] && results[0].data && results[0].data.form;
            var product = null;
            var translations = {};
            if (productID) {
                product = normalizeProduct(results[1] && results[1].data && results[1].data.product);
                if (activeScope.language && results[2] && results[2].data && results[2].data.entries) {
                    translations = results[2].data.entries;
                }
            }
            if (!schema || !schema.fields) {
                msg.innerHTML = '<p role="alert">Failed to load form schema.</p>';
                return;
            }

            var html = "";
            for (var i = 0; i < schema.fields.length; i++) {
                html += renderSchemaField(schema.fields[i], product, translations, !!productID);
            }
            html += renderProductMediaField(product);
            html += '<button type="submit">' + (productID ? 'Save Product' : 'Create Product') + '</button>';
            form.innerHTML = html;

            setupProductMediaPicker(form, product);

            form.addEventListener("submit", function (e) {
                e.preventDefault();
                var payload = collectProductPayload(schema.fields, form, !!productID);
                var method = productID ? "PUT" : "POST";
                var url = productID ? "/admin/products/" + encodeURIComponent(productID) : "/admin/products";
                api(url, { method: method, body: JSON.stringify(payload) }).then(function (body) {
                    if (body && body.error) {
                        msg.innerHTML = '<p role="alert">' + esc(body.error.message || "Save failed") + '</p>';
                        return;
                    }
                    if (!productID) {
                        msg.innerHTML = '<p>Saved.</p>';
                        if (body && body.data && body.data.product && body.data.product.id) {
                            navigateTo("/admin/products/" + body.data.product.id);
                        }
                        return;
                    }
                    saveProductTranslations(productID, activeScope, schema.fields, form, msg);
                }).catch(function () {
                    msg.innerHTML = '<p role="alert">Save failed.</p>';
                });
            });

            if (productID) {
                setupProductCategoryAssignment(productID);
                setupVariantPanel(productID);
            }
        }).catch(function () {
            msg.innerHTML = '<p role="alert">Failed to load product form.</p>';
        });
    }

    function saveProductTranslations(productID, activeScope, fields, form, msg) {
        var entries = collectProductTranslationPayload(fields, form);
        if (!activeScope.language || !entries) {
            msg.innerHTML = '<p>Saved.</p>';
            return;
        }
        api("/admin/products/" + encodeURIComponent(productID) + "/translations", {
            method: "PUT",
            body: JSON.stringify({ entries: entries })
        }).then(function (body) {
            if (body && body.error) {
                msg.innerHTML = '<p role="alert">' + esc(body.error.message || "Save failed") + '</p>';
                return;
            }
            msg.innerHTML = '<p>Saved.</p>';
        }).catch(function () {
            msg.innerHTML = '<p role="alert">Save failed.</p>';
        });
    }

    function productFieldScope(field) {
        if (field && field.meta && typeof field.meta.scope === "string" && field.meta.scope) {
            return field.meta.scope;
        }
        return "global";
    }

    function renderProductFieldScopeBadge(scope) {
        if (scope === "store") {
            return ' <span class="settings-scope-badge settings-scope-badge-store">Store-scoped</span>';
        }
        if (scope === "translatable") {
            return ' <span class="settings-scope-badge settings-scope-badge-translatable">Translatable</span>';
        }
        return ' <span class="settings-scope-badge settings-scope-badge-global">Global</span>';
    }

    function renderProductScopeBanner(scope) {
        var banner = document.getElementById("product-scope-banner");
        if (!banner) {
            return;
        }
        var language = scope && scope.language ? scope.language : "";
        var storeID = scope && scope.storeID ? scope.storeID : "";
        var currency = scope && scope.currency ? scope.currency : "";
        var meta = "";
        if (storeID) {
            meta += ' Store: <strong>' + esc(storeID) + '</strong>.';
        }
        if (currency) {
            meta += ' Currency: <strong>' + esc(currency) + '</strong>.';
        }
        if (language) {
            var priceNote = "";
            if (currency) {
                priceNote = storeID
                    ? " Variant prices edit the store override for this currency."
                    : " Variant prices edit the global/default price for this currency.";
            }
            banner.innerHTML = '<p><strong>Current catalog scope:</strong> Translatable fields edit the <strong>' + esc(language) + '</strong> translation; global fields apply to all stores and languages.' + priceNote + meta + ' Change language in the header switcher to edit another translation.</p>';
            return;
        }
        var priceNoteGlobal = "";
        if (currency) {
            priceNoteGlobal = storeID
                ? " Variant prices edit the store override for this currency."
                : " Variant prices edit the global/default price for this currency.";
        }
        banner.innerHTML = '<p><strong>Current catalog scope:</strong> Global defaults. Select a language in the header switcher to edit per-language translations.' + priceNoteGlobal + meta + '</p>';
    }

    function setupProductCategoryAssignment(productID) {
        var panel = document.getElementById('product-category-panel');
        var msg = document.getElementById('product-category-msg');
        var body = document.getElementById('product-category-body');
        var assignmentState = { search: '', offset: 0, limit: 20 };
        var availableState = { offset: 0, limit: 20 };
        var categoryOrderLookup = {};
        if (!panel || !msg || !body) {
            return;
        }
        panel.style.display = '';

        function setMessage(text, isError) {
            msg.innerHTML = text ? '<p' + (isError ? ' role="alert"' : ' role="status" aria-live="polite"') + '>' + esc(text) + '</p>' : '';
        }

        function loadAssignments() {
            body.innerHTML = '<p>Loading…</p>';
            Promise.all([
                api('/admin/products/' + encodeURIComponent(productID)),
                api('/admin/categories')
            ]).then(function (results) {
                var productBody = results[0] || {};
                var categoriesBody = results[1] || {};
                if (categoriesBody.error && categoriesBody.error.code === 'forbidden') {
                    body.innerHTML = '<p role="alert">Your account does not have categories access, so category assignment is unavailable.</p>';
                    return;
                }

                var categories = normalizeCategoryTree(categoriesBody.data && categoriesBody.data.categories);
                var categoryOptions = flattenCategoryOptions(categories, 0, []);
                var assignedIDs = (productBody.data && productBody.data.category_ids) || [];
                if (!Array.isArray(categories) || !Array.isArray(assignedIDs)) {
                    body.innerHTML = '<p role="alert">' + esc(extractErrorMessage(productBody.error ? productBody : categoriesBody, 'Failed to load assigned categories.')) + '</p>';
                    return;
                }

                var assignedLookup = {};
                for (var i = 0; i < assignedIDs.length; i++) {
                    assignedLookup[String(assignedIDs[i] || '')] = true;
                }
                categoryOrderLookup = {};
                for (var orderIndex = 0; orderIndex < categoryOptions.length; orderIndex++) {
                    var orderedOption = categoryOptions[orderIndex] || {};
                    categoryOrderLookup[String(orderedOption.id || '')] = orderIndex;
                }

                var assignedCategories = [];
                var availableCategories = [];
                for (var j = 0; j < categoryOptions.length; j++) {
                    var option = categoryOptions[j] || {};
                    if (assignedLookup[String(option.id || '')]) {
                        assignedCategories.push(option);
                    } else {
                        availableCategories.push(option);
                    }
                }

                assignmentState.offset = clampPagedOffset(assignmentState.offset, assignmentState.limit, assignedCategories.length);
                availableState.offset = clampPagedOffset(availableState.offset, availableState.limit, availableCategories.length);

                renderProductCategoryAssignmentView(assignedCategories, availableCategories);
            }).catch(function (err) {
                body.innerHTML = '<p role="alert">' + esc(extractErrorMessage(err, 'Failed to load assigned categories.')) + '</p>';
            });
        }

        function renderProductCategoryAssignmentView(assignedCategories, availableCategories) {
            body.innerHTML = renderProductCategoryAssignmentBody(assignedCategories, availableCategories, assignmentState, availableState);
            bindProductCategoryAssignmentActions(productID, body, setMessage, loadAssignments, renderProductCategoryAssignmentView, assignedCategories, availableCategories, assignmentState, availableState, categoryOrderLookup);
        }

        loadAssignments();
    }

    function renderProductCategoryAssignmentBody(assignedCategories, availableCategories, assignmentState, availableState) {
        var searchTerm = assignmentState.search;
        var filteredAssignedCategories = filterCategoryAssignmentOptions(assignedCategories, searchTerm);
        var filteredAvailableCategories = filterCategoryAssignmentOptions(availableCategories, searchTerm);
        assignmentState.offset = clampPagedOffset(assignmentState.offset, assignmentState.limit, filteredAssignedCategories.length);
        availableState.offset = clampPagedOffset(availableState.offset, availableState.limit, filteredAvailableCategories.length);
        var assignedStart = assignmentState.offset;
        var assignedEnd = assignedStart + assignmentState.limit;
        var pagedAssignedCategories = filteredAssignedCategories.slice(assignedStart, assignedEnd);
        var availableStart = availableState.offset;
        var availableEnd = availableStart + availableState.limit;
        var pagedAvailableCategories = filteredAvailableCategories.slice(availableStart, availableEnd);
        var assignedPageNumber = Math.floor(assignedStart / assignmentState.limit) + 1;
        var availablePageNumber = Math.floor(availableStart / availableState.limit) + 1;
        var totalAssignedPages = filteredAssignedCategories.length === 0 ? 1 : Math.ceil(filteredAssignedCategories.length / assignmentState.limit);
        var totalAvailablePages = filteredAvailableCategories.length === 0 ? 1 : Math.ceil(filteredAvailableCategories.length / availableState.limit);
        var hasAssignedPrev = assignedStart > 0;
        var hasAssignedNext = filteredAssignedCategories.length > assignedEnd;
        var hasAvailablePrev = availableStart > 0;
        var hasAvailableNext = filteredAvailableCategories.length > availableEnd;
        var html = '<div style="margin-bottom:1rem">';
        html += '<label>Filter categories<input type="search" id="product-category-search" placeholder="Search categories" value="' + esc(searchTerm || '') + '"></label>';
        if (availableCategories.length > 0 && filteredAvailableCategories.length > 0) {
            html += '<label>Assign category<select id="product-assignment-category">';
            for (var i = 0; i < pagedAvailableCategories.length; i++) {
                var category = pagedAvailableCategories[i] || {};
                html += '<option value="' + esc(String(category.id || '')) + '">' + esc((category.label || category.id || '') + ' (' + (category.slug || category.id || '') + ')') + '</option>';
            }
            html += '</select></label> <button type="button" id="assign-product-category-btn">Assign Category</button>';
        } else if (availableCategories.length > 0) {
            html += '<p class="settings-scope-note">No available categories match this filter.</p>';
        } else {
            html += '<p class="settings-scope-note">All categories are already assigned to this product.</p>';
        }
        html += '<div style="margin-top:0.5rem">';
        html += '<p class="settings-scope-note">Available categories page ' + esc(String(availablePageNumber)) + ' of ' + esc(String(totalAvailablePages)) + '.</p>';
        html += '<button type="button" id="product-available-prev-page"' + (hasAvailablePrev ? '' : ' disabled') + '>Previous Available Categories Page</button> ';
        html += '<button type="button" id="product-available-next-page"' + (hasAvailableNext ? '' : ' disabled') + '>Next Available Categories Page</button>';
        html += '</div>';
        html += '</div>';
        html += '<div style="margin-bottom:0.5rem">';
        html += '<p class="settings-scope-note">Assigned categories page ' + esc(String(assignedPageNumber)) + ' of ' + esc(String(totalAssignedPages)) + '.</p>';
        html += '<button type="button" id="product-assignment-prev-page"' + (hasAssignedPrev ? '' : ' disabled') + '>Previous Assigned Categories Page</button> ';
        html += '<button type="button" id="product-assignment-next-page"' + (hasAssignedNext ? '' : ' disabled') + '>Next Assigned Categories Page</button>';
        html += '</div>';
        html += '<table class="admin-table"><thead><tr><th scope="col">Name</th><th scope="col">Slug</th><th scope="col">Action</th></tr></thead><tbody>';
        if (assignedCategories.length === 0) {
            html += '<tr><td colspan="3">No categories assigned.</td></tr>';
        } else if (filteredAssignedCategories.length === 0) {
            html += '<tr><td colspan="3">No assigned categories match this filter.</td></tr>';
        } else {
            for (var j = 0; j < pagedAssignedCategories.length; j++) {
                var assigned = pagedAssignedCategories[j] || {};
                var categoryLabel = esc((assigned.label || assigned.slug || assigned.id || 'Category'));
                html += '<tr>' +
                    '<td>' + esc(assigned.label || assigned.id || '') + '</td>' +
                    '<td>' + esc(assigned.slug || assigned.id || '') + '</td>' +
                    '<td><button type="button" aria-label="Remove category ' + categoryLabel + '" data-product-category-remove="' + esc(String(assigned.id || '')) + '">Remove</button></td>' +
                    '</tr>';
            }
        }
        html += '</tbody></table>';
        return html;
    }

    function bindProductCategoryAssignmentActions(productID, container, setMessage, reload, rerender, assignedCategories, availableCategories, assignmentState, availableState, categoryOrderLookup) {
        function isMutationBusy() {
            return container.getAttribute('data-product-category-mutation-busy') === '1';
        }

        function setMutationBusy(isBusy) {
            container.setAttribute('data-product-category-mutation-busy', isBusy ? '1' : '0');
            container.setAttribute('aria-busy', isBusy ? 'true' : 'false');
            var assignActionButton = document.getElementById('assign-product-category-btn');
            if (assignActionButton) {
                assignActionButton.disabled = !!isBusy;
            }
            var removeActionButtons = container.querySelectorAll('[data-product-category-remove]');
            for (var idx = 0; idx < removeActionButtons.length; idx++) {
                removeActionButtons[idx].disabled = !!isBusy;
            }
        }

        var searchInput = document.getElementById('product-category-search');
        if (searchInput) {
            searchInput.addEventListener('input', function () {
                assignmentState.search = this.value || '';
                assignmentState.offset = 0;
                availableState.offset = 0;
                rerender(assignedCategories, availableCategories);
            });
        }

        var availablePrevBtn = document.getElementById('product-available-prev-page');
        if (availablePrevBtn) {
            availablePrevBtn.addEventListener('click', function () {
                if (availableState.offset <= 0) {
                    return;
                }
                availableState.offset = Math.max(0, availableState.offset - availableState.limit);
                rerender(assignedCategories, availableCategories);
            });
        }

        var availableNextBtn = document.getElementById('product-available-next-page');
        if (availableNextBtn) {
            availableNextBtn.addEventListener('click', function () {
                availableState.offset += availableState.limit;
                rerender(assignedCategories, availableCategories);
            });
        }

        var prevAssignedBtn = document.getElementById('product-assignment-prev-page');
        if (prevAssignedBtn) {
            prevAssignedBtn.addEventListener('click', function () {
                if (assignmentState.offset <= 0) {
                    return;
                }
                assignmentState.offset = Math.max(0, assignmentState.offset - assignmentState.limit);
                rerender(assignedCategories, availableCategories);
            });
        }

        var nextAssignedBtn = document.getElementById('product-assignment-next-page');
        if (nextAssignedBtn) {
            nextAssignedBtn.addEventListener('click', function () {
                assignmentState.offset += assignmentState.limit;
                rerender(assignedCategories, availableCategories);
            });
        }

        var assignBtn = document.getElementById('assign-product-category-btn');
        if (assignBtn) {
            assignBtn.addEventListener('click', function () {
                if (isMutationBusy()) {
                    return;
                }
                var select = document.getElementById('product-assignment-category');
                var categoryID = select && select.value ? select.value : '';
                if (!categoryID) {
                    return;
                }
                setMessage('Saving category assignment...', false);
                setMutationBusy(true);
                api('/admin/categories/' + encodeURIComponent(categoryID) + '/products/' + encodeURIComponent(productID), { method: 'POST' }).then(function (body) {
                    if (body && body.error) {
                        setMessage(body.error.message || 'Failed to assign category.', true);
                        setMutationBusy(false);
                        return;
                    }
                    setMessage('Category assigned.', false);
                    var movedCategory = null;
                    for (var i = 0; i < availableCategories.length; i++) {
                        var available = availableCategories[i] || {};
                        if (String(available.id || '') === String(categoryID)) {
                            movedCategory = available;
                            availableCategories.splice(i, 1);
                            break;
                        }
                    }
                    if (!movedCategory) {
                        setMutationBusy(false);
                        reload();
                        return;
                    }
                    insertCategoryByOrder(assignedCategories, movedCategory, categoryOrderLookup);
                    availableState.offset = clampPagedOffset(availableState.offset, availableState.limit, availableCategories.length);
                    assignmentState.offset = clampPagedOffset(assignmentState.offset, assignmentState.limit, assignedCategories.length);
                    setMutationBusy(false);
                    rerender(assignedCategories, availableCategories);
                }).catch(function (err) {
                    setMessage(extractErrorMessage(err, 'Failed to assign category.'), true);
                    setMutationBusy(false);
                });
            });
        }

        var removeButtons = container.querySelectorAll('[data-product-category-remove]');
        for (var i = 0; i < removeButtons.length; i++) {
            removeButtons[i].addEventListener('click', function () {
                if (isMutationBusy()) {
                    return;
                }
                var categoryID = this.getAttribute('data-product-category-remove');
                setMessage('Saving category assignment...', false);
                setMutationBusy(true);
                api('/admin/categories/' + encodeURIComponent(categoryID) + '/products/' + encodeURIComponent(productID), { method: 'DELETE' }).then(function (body) {
                    if (body && body.error) {
                        setMessage(body.error.message || 'Failed to remove category.', true);
                        setMutationBusy(false);
                        return;
                    }
                    setMessage('Category removed from product.', false);
                    var movedCategory = null;
                    for (var j = 0; j < assignedCategories.length; j++) {
                        var assigned = assignedCategories[j] || {};
                        if (String(assigned.id || '') === String(categoryID)) {
                            movedCategory = assigned;
                            assignedCategories.splice(j, 1);
                            break;
                        }
                    }
                    if (!movedCategory) {
                        setMutationBusy(false);
                        reload();
                        return;
                    }
                    insertCategoryByOrder(availableCategories, movedCategory, categoryOrderLookup);
                    assignmentState.offset = clampPagedOffset(assignmentState.offset, assignmentState.limit, assignedCategories.length);
                    availableState.offset = clampPagedOffset(availableState.offset, availableState.limit, availableCategories.length);
                    setMutationBusy(false);
                    rerender(assignedCategories, availableCategories);
                }).catch(function (err) {
                    setMessage(extractErrorMessage(err, 'Failed to remove category.'), true);
                    setMutationBusy(false);
                });
            });
        }
    }

    function filterCategoryAssignmentOptions(options, searchTerm) {
        if (!Array.isArray(options)) {
            return [];
        }
        var query = String(searchTerm || '').trim().toLowerCase();
        if (!query) {
            return options.slice();
        }
        var out = [];
        for (var i = 0; i < options.length; i++) {
            var option = options[i] || {};
            var text = ((option.label || '') + ' ' + (option.slug || '') + ' ' + (option.id || '')).toLowerCase();
            if (text.indexOf(query) !== -1) {
                out.push(option);
            }
        }
        return out;
    }

    function insertCategoryByOrder(list, category, orderLookup) {
        if (!Array.isArray(list) || !category) {
            return;
        }
        var categoryID = String(category.id || '');
        if (categoryID) {
            // Ensure local mutation paths cannot duplicate the same category in a target list.
            for (var existingIndex = list.length - 1; existingIndex >= 0; existingIndex--) {
                var existing = list[existingIndex] || {};
                if (String(existing.id || '') === categoryID) {
                    list.splice(existingIndex, 1);
                }
            }
        }
        var targetOrder = orderLookup ? orderLookup[categoryID] : null;
        if (typeof targetOrder !== 'number') {
            list.push(category);
            return;
        }
        for (var i = 0; i < list.length; i++) {
            var current = list[i] || {};
            var currentOrder = orderLookup[String(current.id || '')];
            if (typeof currentOrder !== 'number' || currentOrder > targetOrder) {
                list.splice(i, 0, category);
                return;
            }
        }
        list.push(category);
    }

    function clampPagedOffset(offset, limit, total) {
        var pageLimit = Number(limit || 0);
        if (pageLimit <= 0 || total <= 0) {
            return 0;
        }
        var rawOffset = Number(offset || 0);
        if (rawOffset <= 0) {
            return 0;
        }
        var maxOffset = Math.floor((total - 1) / pageLimit) * pageLimit;
        return rawOffset > maxOffset ? maxOffset : rawOffset;
    }

    function renderSchemaField(field, product, translations, isEdit) {
        var name = field.name;
        var scope = productFieldScope(field);
        var label = esc(field.label || name) + renderProductFieldScopeBadge(scope);
        var value = field.default;
        if (product && product[name] != null) {
            value = product[name];
        } else if (product && product.attributes && product.attributes[name] != null) {
            value = product.attributes[name];
        }
        if (isEdit && scope === "translatable" && translations && typeof translations[name] === "string" && translations[name] !== "") {
            value = translations[name];
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

    function collectProductPayload(fields, form, isEdit) {
        var payload = { attributes: {} };
        for (var i = 0; i < fields.length; i++) {
            var f = fields[i];
            var el = form.elements[f.name];
            if (!el) {
                continue;
            }
            // In edit mode translatable fields are persisted via the per-language
            // translations API, so leave the global product values untouched.
            if (isEdit && productFieldScope(f) === "translatable") {
                continue;
            }
            var v;
            if (f.type === "checkbox") {
                v = !!el.checked;
            } else if (f.type === "number") {
                v = el.value === "" ? null : parseFloat(el.value);
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

    function collectProductTranslationPayload(fields, form) {
        var entries = {};
        var has = false;
        for (var i = 0; i < fields.length; i++) {
            var f = fields[i];
            if (productFieldScope(f) !== "translatable") {
                continue;
            }
            var el = form.elements[f.name];
            if (!el) {
                continue;
            }
            entries[f.name] = el.value;
            has = true;
        }
        return has ? entries : null;
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
        var currency = adminScope.currency || '';
        var storeID = adminScope.store_id || '';
        var priceScopeBadge = storeID
            ? renderProductFieldScopeBadge('store')
            : renderProductFieldScopeBadge('global');
        var priceHeader = currency ? ('Price (minor units, ' + esc(currency) + ')' + priceScopeBadge) : ('Price' + priceScopeBadge);
        var html = '<table><thead><tr><th scope="col">SKU</th><th scope="col">Name</th><th scope="col">Weight</th><th scope="col">' + priceHeader + '</th><th scope="col">Action</th></tr></thead><tbody>';
        for (var i = 0; i < variants.length; i++) {
            var v = variants[i];
            var variantLabel = esc((v.sku || v.name || v.id || 'variant'));
            html += '<tr data-variant-id="' + esc(v.id) + '">';
            html += '<td><input data-field="sku" value="' + esc(v.sku || '') + '"></td>';
            html += '<td><input data-field="name" value="' + esc(v.name || '') + '"></td>';
            html += '<td><input data-field="weight" type="number" step="0.01" min="0" value="' + esc(v.weight == null ? '' : String(v.weight)) + '"></td>';
            if (currency) {
                html += '<td><input data-field="price" type="number" step="1" min="1" aria-label="Store price for ' + variantLabel + '"><span class="variant-price-scope-hint settings-scope-note"></span> <button type="button" aria-label="Save price ' + variantLabel + '" class="variant-price-save-btn">Save Price</button></td>';
            } else {
                html += '<td><span class="settings-scope-note">Select a currency context to edit price.</span></td>';
            }
            html += '<td><button type="button" aria-label="Save variant ' + variantLabel + '" class="variant-save-btn">Save</button></td>';
            html += '</tr>';
        }
        html += '</tbody></table>';
        if (currency) {
            var scopeText = storeID ? ('store override for ' + esc(storeID)) : 'the global/default price';
            html += '<p class="settings-scope-note">Prices save as ' + scopeText + ' in <strong>' + esc(currency) + '</strong>. Change store or currency in the header switcher to edit another scope.</p>';
        }
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

        if (currency) {
            loadVariantPrices(container, productID);
            bindVariantPriceSave(container, productID);
        }
    }

    function setVariantPriceScopeHint(row, scope, fallbackAmount) {
        var hint = row.querySelector(".variant-price-scope-hint");
        if (!hint) {
            return;
        }
        if (scope === "store") {
            hint.textContent = "Store override";
            return;
        }
        if (scope === "global") {
            hint.textContent = "Global price";
            return;
        }
        if (fallbackAmount != null) {
            hint.textContent = "No store override; global fallback " + fallbackAmount;
            return;
        }
        hint.textContent = "No price set for this scope";
    }

    function loadVariantPrices(container, productID) {
        var rows = container.querySelectorAll('tr[data-variant-id]');
        var msg = document.getElementById("variant-msg");
        var errors = 0;
        var pending = rows.length;
        if (pending === 0) {
            return;
        }

        function checkDone() {
            pending -= 1;
            if (pending === 0 && errors > 0 && msg) {
                msg.innerHTML = '<p role="alert">Failed to load one or more variant prices.</p>';
            }
        }

        for (var i = 0; i < rows.length; i++) {
            (function (row) {
                var variantID = row.getAttribute('data-variant-id');
                var input = row.querySelector('[data-field="price"]');
                if (!input) {
                    checkDone();
                    return;
                }
                api("/admin/products/" + encodeURIComponent(productID) + "/variants/" + encodeURIComponent(variantID) + "/price").then(function (body) {
                    var data = body && body.data;
                    if (!data) {
                        checkDone();
                        return;
                    }
                    if (data.price && data.price.amount != null) {
                        input.value = String(data.price.amount);
                        input.placeholder = "";
                        setVariantPriceScopeHint(row, data.price_scope || (data.price.store_id ? "store" : "global"));
                    } else if (data.global_fallback && data.global_fallback.amount != null) {
                        input.value = "";
                        input.placeholder = String(data.global_fallback.amount);
                        setVariantPriceScopeHint(row, "unset", data.global_fallback.amount);
                    } else {
                        input.value = "";
                        input.placeholder = "";
                        setVariantPriceScopeHint(row, data.price_scope || "unset");
                    }
                    checkDone();
                }).catch(function () {
                    errors += 1;
                    setVariantPriceScopeHint(row, "unset");
                    checkDone();
                });
            })(rows[i]);
        }
    }

    function bindVariantPriceSave(container, productID) {
        var buttons = container.querySelectorAll('.variant-price-save-btn');
        for (var j = 0; j < buttons.length; j++) {
            buttons[j].addEventListener("click", function (e) {
                var row = e.target.closest("tr");
                var variantID = row.getAttribute("data-variant-id");
                var input = row.querySelector('[data-field="price"]');
                var msg = document.getElementById("variant-msg");
                if (!input || input.value === "") {
                    if (msg) {
                        msg.innerHTML = '<p role="alert">Enter a price amount in minor units.</p>';
                    }
                    return;
                }
                api("/admin/products/" + encodeURIComponent(productID) + "/variants/" + encodeURIComponent(variantID) + "/price", {
                    method: "PUT",
                    body: JSON.stringify({ amount: Number(input.value) })
                }).then(function (body) {
                    if (body && body.error) {
                        if (msg) {
                            msg.innerHTML = '<p role="alert">' + esc(body.error.message || "Save price failed") + '</p>';
                        }
                        return;
                    }
                    if (body && body.data && body.data.price && body.data.price.amount != null) {
                        input.value = String(body.data.price.amount);
                        input.placeholder = "";
                        setVariantPriceScopeHint(row, body.data.price_scope || "store");
                    }
                    if (msg) {
                        msg.innerHTML = '<p>Price saved.</p>';
                    }
                }).catch(function () {
                    if (msg) {
                        msg.innerHTML = '<p role="alert">Save price failed.</p>';
                    }
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

    function normalizeCustomers(customers) {
        if (!Array.isArray(customers)) {
            return [];
        }
        var out = [];
        for (var i = 0; i < customers.length; i++) {
            var customer = normalizeCustomer(customers[i]);
            if (customer) {
                out.push(customer);
            }
        }
        return out;
    }

    function normalizeCustomer(raw) {
        if (!raw) {
            return null;
        }
        return {
            id: pick(raw, "id", "ID"),
            email: pick(raw, "email", "Email"),
            first_name: pick(raw, "first_name", "FirstName"),
            last_name: pick(raw, "last_name", "LastName"),
            role: pick(raw, "role", "Role"),
            status: pick(raw, "status", "Status"),
            email_verified_at: pick(raw, "email_verified_at", "EmailVerifiedAt"),
            created_at: pick(raw, "created_at", "CreatedAt"),
            updated_at: pick(raw, "updated_at", "UpdatedAt")
        };
    }

    function normalizePages(pages) {
        if (!Array.isArray(pages)) {
            return [];
        }
        var out = [];
        for (var i = 0; i < pages.length; i++) {
            var page = normalizePage(pages[i]);
            if (page) {
                out.push(page);
            }
        }
        return out;
    }

    function normalizePage(raw) {
        if (!raw) {
            return null;
        }
        return {
            id: pick(raw, "id", "ID"),
            slug: pick(raw, "slug", "Slug"),
            title: pick(raw, "title", "Title"),
            content: pick(raw, "content", "Content"),
            language: pick(raw, "language", "Language") || "",
            is_active: !!pick(raw, "is_active", "IsActive"),
            created_at: pick(raw, "created_at", "CreatedAt"),
            updated_at: pick(raw, "updated_at", "UpdatedAt")
        };
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

    function emptyAdminScope() {
        return { store_id: "", language: "", currency: "" };
    }

    function loadAdminScope() {
        try {
            var raw = localStorage.getItem(ADMIN_SCOPE_KEY);
            if (!raw) {
                return emptyAdminScope();
            }
            var parsed = JSON.parse(raw);
            return {
                store_id: parsed && typeof parsed.store_id === "string" ? parsed.store_id : "",
                language: parsed && typeof parsed.language === "string" ? parsed.language : "",
                currency: parsed && typeof parsed.currency === "string" ? parsed.currency : ""
            };
        } catch (err) {
            return emptyAdminScope();
        }
    }

    function saveAdminScope() {
        localStorage.setItem(ADMIN_SCOPE_KEY, JSON.stringify(adminScope));
    }

    function uniqueValues(stores, key) {
        var seen = {};
        var out = [];
        for (var i = 0; i < stores.length; i++) {
            var value = stores[i] && stores[i][key] ? String(stores[i][key]) : "";
            if (!value || seen[value]) {
                continue;
            }
            seen[value] = true;
            out.push(value);
        }
        return out;
    }

    function findStoreByID(stores, storeID) {
        for (var i = 0; i < stores.length; i++) {
            if (stores[i] && stores[i].id === storeID) {
                return stores[i];
            }
        }
        return null;
    }

    function ensureValidAdminScope(stores) {
        if (!stores || stores.length === 0) {
            adminScope = emptyAdminScope();
            saveAdminScope();
            return;
        }

        var selectedStore = findStoreByID(stores, adminScope.store_id);
        if (!selectedStore) {
            selectedStore = choosePrimaryStore(stores);
            adminScope.store_id = selectedStore && selectedStore.id ? selectedStore.id : "";
        }

        if (!adminScope.language) {
            adminScope.language = selectedStore && selectedStore.language ? selectedStore.language : "";
        }
        if (!adminScope.currency) {
            adminScope.currency = selectedStore && selectedStore.currency ? selectedStore.currency : "";
        }

        if (selectedStore && selectedStore.language) {
            adminScope.language = selectedStore.language;
        }
        if (selectedStore && selectedStore.currency) {
            adminScope.currency = selectedStore.currency;
        }

        saveAdminScope();
    }

    function renderContextSelect(select, options, selectedValue, fallbackLabel) {
        if (!select) {
            return;
        }
        var html = "";
        if (options.length === 0) {
            html = '<option value="">' + esc(fallbackLabel) + '</option>';
        } else {
            for (var i = 0; i < options.length; i++) {
                var option = options[i];
                var selected = option.value === selectedValue ? ' selected' : '';
                html += '<option value="' + esc(option.value) + '"' + selected + '>' + esc(option.label) + '</option>';
            }
        }
        select.innerHTML = html;
        select.disabled = options.length === 0;
    }

    function renderContextSwitcher() {
        var storeSelect = document.getElementById("admin-context-store");
        var languageSelect = document.getElementById("admin-context-language");
        var currencySelect = document.getElementById("admin-context-currency");
        if (!storeSelect || !languageSelect || !currencySelect) {
            return;
        }

        var storeOptions = [];
        for (var i = 0; i < adminScopeStores.length; i++) {
            var store = adminScopeStores[i];
            storeOptions.push({
                value: store.id,
                label: (store.name || store.code || store.id) + (store.code ? " (" + store.code + ")" : "")
            });
        }
        renderContextSelect(storeSelect, storeOptions, adminScope.store_id, "No stores");

        var languages = uniqueValues(adminScopeStores, "language");
        var languageOptions = [];
        for (var j = 0; j < languages.length; j++) {
            languageOptions.push({ value: languages[j], label: languages[j] });
        }
        renderContextSelect(languageSelect, languageOptions, adminScope.language, "No languages");

        var currencies = uniqueValues(adminScopeStores, "currency");
        var currencyOptions = [];
        for (var k = 0; k < currencies.length; k++) {
            currencyOptions.push({ value: currencies[k], label: currencies[k] });
        }
        renderContextSelect(currencySelect, currencyOptions, adminScope.currency, "No currencies");
    }

    function loadContextSwitcherData() {
        if (!isAuthenticated()) {
            adminScopeStores = [];
            renderContextSwitcher();
            return Promise.resolve();
        }

        return api("/admin/stores").then(function (body) {
            adminScopeStores = normalizeStores(body && body.data && body.data.stores ? body.data.stores : []);
            ensureValidAdminScope(adminScopeStores);
            renderContextSwitcher();
        }).catch(function () {
            adminScopeStores = [];
            renderContextSwitcher();
        });
    }

    function bindContextSwitcher() {
        var storeSelect = document.getElementById("admin-context-store");
        var languageSelect = document.getElementById("admin-context-language");
        var currencySelect = document.getElementById("admin-context-currency");
        if (!storeSelect || !languageSelect || !currencySelect) {
            return;
        }

        storeSelect.addEventListener("change", function () {
            adminScope.store_id = storeSelect.value;
            var store = findStoreByID(adminScopeStores, adminScope.store_id);
            if (store && store.language) {
                adminScope.language = store.language;
            }
            if (store && store.currency) {
                adminScope.currency = store.currency;
            }
            saveAdminScope();
            renderContextSwitcher();
            handleRoute();
        });

        languageSelect.addEventListener("change", function () {
            adminScope.language = languageSelect.value;
            saveAdminScope();
            handleRoute();
        });

        currencySelect.addEventListener("change", function () {
            adminScope.currency = currencySelect.value;
            saveAdminScope();
            handleRoute();
        });
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
            var active = href !== "/admin" &&
                currentPath.indexOf(href) === 0 &&
                (currentPath.length === href.length || currentPath.charAt(href.length) === "/");
            if (active) {
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
	        if (currentUser) {
	            el.textContent = currentUser.first_name || currentUser.email || "Account";
	        } else {
	            el.textContent = "Account";
	        }
	    } else {
	        el.textContent = "";
        }
    }

    function loadCurrentUser() {
        if (!isAuthenticated()) {
            currentUser = null;
            updateUserInfo();
            return Promise.resolve(null);
        }
        return api("/auth/me").then(function (body) {
            currentUser = body && body.data ? body.data : null;
            updateUserInfo();
            return currentUser;
        }).catch(function () {
            currentUser = null;
            updateUserInfo();
            return null;
        });
    }

    function hasAdminPanelAccess(role) {
        return role === "admin" || role === "manager" || role === "editor" || role === "support";
    }

    function adminRoleDefinitions() {
        return [
            {
                role: 'admin',
                label: 'Administrator',
                permissions: [
                    'products.read', 'products.write',
                    'orders.read', 'orders.write',
                    'categories.read', 'categories.write',
                    'customers.read', 'customers.write',
                    'invoices.read',
                    'media.read', 'media.write',
                    'settings.read', 'settings.write',
                    'audit.read',
                    'shipping.read', 'shipping.write'
                ]
            },
            {
                role: 'manager',
                label: 'Manager',
                permissions: [
                    'products.read', 'products.write',
                    'orders.read', 'orders.write',
                    'categories.read', 'categories.write',
                    'customers.read',
                    'invoices.read',
                    'media.read', 'media.write',
                    'shipping.read', 'shipping.write'
                ]
            },
            {
                role: 'editor',
                label: 'Editor',
                permissions: [
                    'products.read', 'products.write',
                    'categories.read', 'categories.write',
                    'media.read', 'media.write'
                ]
            },
            {
                role: 'support',
                label: 'Support',
                permissions: [
                    'products.read',
                    'orders.read',
                    'customers.read',
                    'invoices.read'
                ]
            }
        ];
    }

    // --- Pages ---

    function renderLogin(container) {
        var html =
            '<div class="login-container">' +
            "<h1>Admin Login</h1>" +
            '<p class="login-note">Use an admin account created during setup or seeding. This form does not show default credentials.</p>' +
            '<form id="login-form" autocomplete="off">' +
            "<label>Email<input type=\"email\" name=\"email\" required autocomplete=\"off\" autocapitalize=\"off\" spellcheck=\"false\"></label>" +
            "<label>Password<input type=\"password\" name=\"password\" required autocomplete=\"off\"></label>" +
            '<div id="login-error" role="alert"></div>' +
            "<button type=\"submit\">Sign In</button>" +
            "</form>" +
            "</div>";
        container.innerHTML = html;

        var form = document.getElementById("login-form");
        var initialMessage = popLoginMessage();
        var errBox = document.getElementById("login-error");
        if (initialMessage) {
            errBox.textContent = initialMessage;
        }
        form.addEventListener("submit", function (e) {
            e.preventDefault();
            var email = form.elements.email.value;
            var password = form.elements.password.value;
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
                    loadCurrentUser().then(function (user) {
                        if (!user || !hasAdminPanelAccess(user.role)) {
                            clearToken();
                            errBox.textContent = "This account has no admin permissions.";
                            return;
                        }
                        loadContextSwitcherData().then(function () {
                            navigateTo("/admin/dashboard");
                        });
                    }).catch(function () {
                        clearToken();
                        errBox.textContent = "Failed to verify admin permissions";
                    });
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

    function renderAdminAccount(container) {
        container.innerHTML = '' +
            '<h2>Account</h2>' +
            '<p>Update your admin name and password for the current signed-in account.</p>' +
            '<div id="admin-account-profile-msg"></div>' +
            '<form id="admin-account-profile-form">' +
            '<label>Email<input name="email" type="email" disabled></label>' +
            '<label>First Name<input name="first_name"></label>' +
            '<label>Last Name<input name="last_name"></label>' +
            '<button type="submit">Save Account Details</button>' +
            '</form>' +
            '<hr>' +
            '<div id="admin-account-password-msg"></div>' +
            '<form id="admin-account-password-form">' +
            '<label>Current Password<input name="current_password" type="password" autocomplete="current-password" required></label>' +
            '<label>New Password<input name="new_password" type="password" autocomplete="new-password" minlength="8" required></label>' +
            '<button type="submit">Change Password</button>' +
            '</form>';

        var profileForm = document.getElementById("admin-account-profile-form");
        var passwordForm = document.getElementById("admin-account-password-form");
        var profileMsg = document.getElementById("admin-account-profile-msg");
        var passwordMsg = document.getElementById("admin-account-password-msg");

        loadCurrentUser().then(function (user) {
            if (!user) {
                profileMsg.innerHTML = '<p role="alert">Failed to load account details.</p>';
                return;
            }
            profileForm.elements.email.value = user.email || "";
            profileForm.elements.first_name.value = user.first_name || "";
            profileForm.elements.last_name.value = user.last_name || "";
        });

        profileForm.addEventListener("submit", function (e) {
            e.preventDefault();
            profileMsg.innerHTML = "";
            api("/auth/me/profile", {
                method: "PUT",
                body: JSON.stringify({
                    first_name: profileForm.elements.first_name.value,
                    last_name: profileForm.elements.last_name.value
                })
            }).then(function (body) {
                if (body && body.error) {
                    profileMsg.innerHTML = '<p role="alert">' + esc(body.error.message || 'Failed to update account details.') + '</p>';
                    return;
                }
                currentUser = body && body.data ? body.data : currentUser;
                updateUserInfo();
                profileMsg.innerHTML = '<p>Account details saved.</p>';
            }).catch(function (err) {
                profileMsg.innerHTML = '<p role="alert">' + esc(extractErrorMessage(err, 'Failed to update account details.')) + '</p>';
            });
        });

        passwordForm.addEventListener("submit", function (e) {
            e.preventDefault();
            passwordMsg.innerHTML = "";
            api("/auth/me/password", {
                method: "POST",
                body: JSON.stringify({
                    current_password: passwordForm.elements.current_password.value,
                    new_password: passwordForm.elements.new_password.value
                })
            }).then(function (body) {
                if (body && body.error) {
                    passwordMsg.innerHTML = '<p role="alert">' + esc(body.error.message || 'Failed to change password.') + '</p>';
                    return;
                }
                passwordForm.reset();
                passwordMsg.innerHTML = '<p>Password changed.</p>';
            }).catch(function (err) {
                passwordMsg.innerHTML = '<p role="alert">' + esc(extractErrorMessage(err, 'Failed to change password.')) + '</p>';
            });
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
                '<th scope="col">ID</th><th scope="col">Customer</th><th scope="col">Total</th><th scope="col">Status</th><th scope="col">Payment</th><th scope="col">Date</th><th scope="col">Action</th>' +
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

    function renderCustomersGrid(container) {
        container.innerHTML = '<h2>Customers</h2><div id="customers-grid"></div>';

        var grid = document.getElementById("customers-grid");
        api("/admin/customers?offset=0&limit=50").then(function (body) {
            if (body && body.error && body.error.code === "forbidden") {
                grid.innerHTML = '<p role="alert">Your account does not have customer access.</p>';
                return;
            }

            var customersRaw = body && body.data && body.data.customers;
            if (!Array.isArray(customersRaw)) {
                grid.innerHTML = '<p role="alert">' + esc(extractErrorMessage(body, 'Failed to load customers.')) + '</p>';
                return;
            }

            var customers = normalizeCustomers(customersRaw);
            var html = '<table><thead><tr>' +
                '<th>Name</th><th>Email</th><th>Role</th><th>Status</th><th>Verified</th><th>Created</th>' +
                '</tr></thead><tbody>';

            if (customers.length === 0) {
                html += '<tr><td colspan="6">No customers found.</td></tr>';
            } else {
                for (var i = 0; i < customers.length; i++) {
                    var customer = customers[i];
                    var fullName = ((customer.first_name || '') + ' ' + (customer.last_name || '')).trim();
                    html += '<tr>' +
                        '<td><a href="/admin/customers/' + esc(customer.id || '') + '" data-link>' + esc(fullName || customer.email || customer.id || '') + '</a></td>' +
                        '<td>' + esc(customer.email || '') + '</td>' +
                        '<td>' + esc(customer.role || '') + '</td>' +
                        '<td><span class="badge badge-' + esc(customer.status || 'unknown') + '">' + esc(customer.status || 'unknown') + '</span></td>' +
                        '<td>' + esc(customer.email_verified_at ? 'Verified' : 'Pending') + '</td>' +
                        '<td>' + esc(customer.created_at ? String(customer.created_at).substring(0, 10) : '') + '</td>' +
                        '</tr>';
                }
            }

            html += '</tbody></table>';
            grid.innerHTML = html;
        }).catch(function (err) {
            grid.innerHTML = '<p role="alert">' + esc(extractErrorMessage(err, 'Failed to load customers.')) + '</p>';
        });
    }

    function renderUsersRolesPage(container) {
        container.innerHTML = '' +
            '<h2>Users & Roles</h2>' +
            '<div class="settings-grid">' +
            '<section><h3>Admin Users</h3><div id="users-roles-users-grid"></div></section>' +
            '<section><h3>Role Capabilities</h3><div id="users-roles-roles-grid"></div></section>' +
            '</div>';

        var usersGrid = document.getElementById('users-roles-users-grid');
        var rolesGrid = document.getElementById('users-roles-roles-grid');
        var roleDefs = adminRoleDefinitions();
        var rolesHtml = '<p class="settings-scope-note">Role capabilities reflect the current core RBAC model.</p>' +
            '<table><thead><tr><th>Role</th><th>Permissions</th></tr></thead><tbody>';

        for (var i = 0; i < roleDefs.length; i++) {
            var roleDef = roleDefs[i];
            rolesHtml += '<tr>' +
                '<td>' + esc(roleDef.label) + '</td>' +
                '<td>' + esc(roleDef.permissions.join(', ')) + '</td>' +
                '</tr>';
        }
        rolesHtml += '</tbody></table>';
        rolesGrid.innerHTML = rolesHtml;

        api('/admin/customers?offset=0&limit=50').then(function (body) {
            if (body && body.error && body.error.code === 'forbidden') {
                usersGrid.innerHTML = '<p role="alert">Your account does not have customer access.</p>';
                return;
            }

            var customersRaw = body && body.data && body.data.customers;
            if (!Array.isArray(customersRaw)) {
                usersGrid.innerHTML = '<p role="alert">' + esc(extractErrorMessage(body, 'Failed to load users and roles.')) + '</p>';
                return;
            }

            var customers = normalizeCustomers(customersRaw);
            var adminUsers = [];
            for (var j = 0; j < customers.length; j++) {
                if (hasAdminPanelAccess(customers[j] && customers[j].role)) {
                    adminUsers.push(customers[j]);
                }
            }

            var usersHtml = '<table><thead><tr>' +
                '<th>Name</th><th>Email</th><th>Role</th><th>Status</th><th>Verified</th>' +
                '</tr></thead><tbody>';

            if (adminUsers.length === 0) {
                usersHtml += '<tr><td colspan="5">No admin users found in the current customer page.</td></tr>';
            } else {
                for (var k = 0; k < adminUsers.length; k++) {
                    var user = adminUsers[k];
                    var fullName = ((user.first_name || '') + ' ' + (user.last_name || '')).trim();
                    usersHtml += '<tr>' +
                        '<td>' + esc(fullName || user.email || user.id || '') + '</td>' +
                        '<td>' + esc(user.email || '') + '</td>' +
                        '<td>' + esc(user.role || '') + '</td>' +
                        '<td><span class="badge badge-' + esc(user.status || 'unknown') + '">' + esc(user.status || 'unknown') + '</span></td>' +
                        '<td>' + esc(user.email_verified_at ? 'Verified' : 'Pending') + '</td>' +
                        '</tr>';
                }
            }

            usersHtml += '</tbody></table>';
            usersGrid.innerHTML = usersHtml;
        }).catch(function (err) {
            usersGrid.innerHTML = '<p role="alert">' + esc(extractErrorMessage(err, 'Failed to load users and roles.')) + '</p>';
        });
    }

    function renderAuditLogPage(container) {
        container.innerHTML = '<h2>Audit Log</h2><div id="audit-log-grid"></div>';
        var grid = document.getElementById('audit-log-grid');
        api('/admin/audit?offset=0&limit=50').then(function (body) {
            if (body && body.error && body.error.code === 'forbidden') {
                grid.innerHTML = '<p role="alert">Your account does not have audit log access.</p>';
                return;
            }
            var entries = body && body.data && body.data.entries;
            if (!Array.isArray(entries)) {
                grid.innerHTML = '<p role="alert">Failed to load audit log.</p>';
                return;
            }
            if (entries.length === 0) {
                grid.innerHTML = '<p>No audit entries yet.</p>';
                return;
            }
            var html = '<table><thead><tr>' +
                '<th>Time</th><th>Admin</th><th>Action</th><th>Resource</th><th>Scope</th><th>Result</th>' +
                '</tr></thead><tbody>';
            for (var i = 0; i < entries.length; i++) {
                var entry = entries[i];
                var scope = [entry.store_id, entry.language, entry.currency].filter(Boolean).join(' / ');
                var resource = (entry.resource_type || '') + (entry.resource_id ? (' #' + entry.resource_id) : '');
                html += '<tr>' +
                    '<td>' + esc(entry.created_at || '') + '</td>' +
                    '<td>' + esc(entry.admin_id || '') + '</td>' +
                    '<td>' + esc(entry.action || '') + '</td>' +
                    '<td>' + esc(resource) + '</td>' +
                    '<td>' + esc(scope || '—') + '</td>' +
                    '<td>' + esc(entry.result || '') + '</td>' +
                    '</tr>';
            }
            html += '</tbody></table>';
            grid.innerHTML = html;
        }).catch(function () {
            grid.innerHTML = '<p role="alert">Failed to load audit log.</p>';
        });
    }

    function renderPromotionsGrid(container) {
        container.innerHTML = '<h2>Promotions</h2><div id="promotions-grid"></div>';

        var grid = document.getElementById("promotions-grid");
        api("/admin/promotions?offset=0&limit=50").then(function (body) {
            if (body && body.error && body.error.code === "forbidden") {
                grid.innerHTML = '<p role="alert">Your account does not have products access.</p>';
                return;
            }

            var promotionsRaw = body && body.data && body.data.promotions;
            if (!Array.isArray(promotionsRaw)) {
                grid.innerHTML = '<p role="alert">' + esc(extractErrorMessage(body, 'Failed to load promotions.')) + '</p>';
                return;
            }

            var html = '<div style="margin-bottom:1rem"><a class="button" href="/admin/marketing/promotions/new" data-link id="new-promotion-btn">New Promotion</a></div>';
            html += '<table><thead><tr>' +
                '<th>Name</th><th>Type</th><th>Discount</th><th>Status</th><th>Updated</th>' +
                '</tr></thead><tbody>';

            if (promotionsRaw.length === 0) {
                html += '<tr><td colspan="5">No promotions found.</td></tr>';
            } else {
                for (var i = 0; i < promotionsRaw.length; i++) {
                    var promo = promotionsRaw[i];
                    var editHref = '/admin/marketing/promotions/' + encodeURIComponent(promo.id || '');
                    var discount = promo.action_type === 'fixed'
                        ? esc(String(promo.action_amount || 0)) + ' minor units'
                        : esc(String(promo.action_percentage || 0)) + '%';
                    html += '<tr>' +
                        '<td><a href="' + editHref + '" data-link>' + esc(promo.name || promo.id || '') + '</a></td>' +
                        '<td>' + esc(promo.type || '') + '</td>' +
                        '<td>' + discount + '</td>' +
                        '<td><span class="badge badge-' + esc(promo.active ? 'active' : 'draft') + '">' + esc(promo.active ? 'active' : 'inactive') + '</span></td>' +
                        '<td>' + esc(promo.updated_at ? String(promo.updated_at).substring(0, 10) : '') + '</td>' +
                        '</tr>';
                }
            }

            html += '</tbody></table>';
            grid.innerHTML = html;
        }).catch(function (err) {
            grid.innerHTML = '<p role="alert">' + esc(extractErrorMessage(err, 'Failed to load promotions.')) + '</p>';
        });
    }

    function renderPromotionCreate(container) {
        renderPromotionForm(container, null);
    }

    function renderPromotionEdit(container, promotionID) {
        renderPromotionForm(container, promotionID);
    }

    function renderPromotionFormFields(promotion) {
        var type = promotion ? (promotion.type || 'catalog') : 'catalog';
        var conditionType = promotion ? (promotion.condition_type || 'always') : 'always';
        var actionType = promotion ? (promotion.action_type || 'percentage') : 'percentage';
        var isActive = promotion ? !!promotion.active : true;
        var couponBound = promotion ? !!promotion.coupon_bound : false;
        return '' +
            '<label>Name<input name="name" value="' + esc(promotion ? (promotion.name || '') : '') + '" required></label>' +
            '<label>Type<select name="type">' +
            '<option value="catalog"' + (type === 'catalog' ? ' selected' : '') + '>Catalog (line item)</option>' +
            '<option value="cart"' + (type === 'cart' ? ' selected' : '') + '>Cart (order total)</option>' +
            '</select></label>' +
            '<label>Priority<input name="priority" type="number" value="' + esc(promotion ? String(promotion.priority || 0) : '0') + '"></label>' +
            '<label>Condition<select name="condition_type">' +
            '<option value="always"' + (conditionType === 'always' ? ' selected' : '') + '>Always</option>' +
            '<option value="min_quantity"' + (conditionType === 'min_quantity' ? ' selected' : '') + '>Minimum quantity (catalog)</option>' +
            '<option value="min_cart_total"' + (conditionType === 'min_cart_total' ? ' selected' : '') + '>Minimum cart total (cart)</option>' +
            '</select></label>' +
            '<label>Condition value<input name="condition_value" type="number" min="0" value="' + esc(promotion ? String(promotion.condition_value || 0) : '0') + '"></label>' +
            '<p class="hint">For min_cart_total use minor currency units (e.g. 5000 = $50.00).</p>' +
            '<label>Discount type<select name="action_type">' +
            '<option value="percentage"' + (actionType === 'percentage' ? ' selected' : '') + '>Percentage</option>' +
            '<option value="fixed"' + (actionType === 'fixed' ? ' selected' : '') + '>Fixed amount</option>' +
            '</select></label>' +
            '<label>Percentage<input name="action_percentage" type="number" min="1" max="100" value="' + esc(promotion ? String(promotion.action_percentage || 10) : '10') + '"></label>' +
            '<label>Fixed amount (minor units)<input name="action_amount" type="number" min="0" value="' + esc(promotion ? String(promotion.action_amount || 0) : '0') + '"></label>' +
            '<label>Start at<input name="start_at" value="' + esc(promotion && promotion.start_at ? String(promotion.start_at).substring(0, 16) : '') + '" placeholder="2026-01-01T00:00"></label>' +
            '<label>End at<input name="end_at" value="' + esc(promotion && promotion.end_at ? String(promotion.end_at).substring(0, 16) : '') + '" placeholder="2026-12-31T23:59"></label>' +
            '<label><input type="checkbox" name="active"' + (isActive ? ' checked' : '') + '> Active</label>' +
            '<label><input type="checkbox" name="coupon_bound"' + (couponBound ? ' checked' : '') + '> Requires coupon code</label>' +
            '<div style="margin-top:1rem"><button type="submit">Save</button>' +
            (promotion ? ' <button type="button" id="delete-promotion-btn" class="danger">Delete</button>' : '') +
            '</div>';
    }

    function renderPromotionForm(container, promotionID) {
        var title = promotionID ? 'Edit Promotion' : 'New Promotion';
        container.innerHTML =
            '<h2>' + title + '</h2>' +
            '<p><a href="/admin/marketing/promotions" data-link>Back to promotions</a></p>' +
            '<div id="promotion-form-msg"></div>' +
            '<form id="promotion-form"><p>Loading…</p></form>';

        var msg = document.getElementById('promotion-form-msg');
        var form = document.getElementById('promotion-form');

        function bindForm(promotion) {
            form.innerHTML = renderPromotionFormFields(promotion);

            form.addEventListener('submit', function (e) {
                e.preventDefault();
                var payload = {
                    name: form.elements.name.value,
                    type: form.elements.type.value,
                    priority: parseInt(form.elements.priority.value, 10) || 0,
                    condition_type: form.elements.condition_type.value,
                    condition_value: parseInt(form.elements.condition_value.value, 10) || 0,
                    action_type: form.elements.action_type.value,
                    action_percentage: parseInt(form.elements.action_percentage.value, 10) || 0,
                    action_amount: parseInt(form.elements.action_amount.value, 10) || 0,
                    active: form.elements.active.checked,
                    coupon_bound: form.elements.coupon_bound.checked
                };
                if (form.elements.start_at.value) {
                    payload.start_at = form.elements.start_at.value.length === 16
                        ? form.elements.start_at.value + ':00Z'
                        : form.elements.start_at.value;
                }
                if (form.elements.end_at.value) {
                    payload.end_at = form.elements.end_at.value.length === 16
                        ? form.elements.end_at.value + ':00Z'
                        : form.elements.end_at.value;
                }
                var method = promotionID ? 'PUT' : 'POST';
                var url = promotionID ? '/admin/promotions/' + encodeURIComponent(promotionID) : '/admin/promotions';
                api(url, { method: method, body: JSON.stringify(payload) }).then(function (body) {
                    if (body && body.error) {
                        msg.innerHTML = '<p role="alert">' + esc(body.error.message || 'Save failed.') + '</p>';
                        return;
                    }
                    navigate('/admin/marketing/promotions');
                }).catch(function (err) {
                    msg.innerHTML = '<p role="alert">' + esc(extractErrorMessage(err, 'Save failed.')) + '</p>';
                });
            });

            if (promotionID) {
                var deleteBtn = document.getElementById('delete-promotion-btn');
                if (deleteBtn) {
                    deleteBtn.addEventListener('click', function () {
                        if (!window.confirm('Delete this promotion? Linked coupons will also be removed.')) {
                            return;
                        }
                        api('/admin/promotions/' + encodeURIComponent(promotionID), { method: 'DELETE' }).then(function (body) {
                            if (body && body.error) {
                                msg.innerHTML = '<p role="alert">' + esc(body.error.message || 'Failed to delete promotion.') + '</p>';
                                return;
                            }
                            navigate('/admin/marketing/promotions');
                        }).catch(function (err) {
                            msg.innerHTML = '<p role="alert">' + esc(extractErrorMessage(err, 'Failed to delete promotion.')) + '</p>';
                        });
                    });
                }
            }
        }

        if (!promotionID) {
            bindForm(null);
            return;
        }

        api('/admin/promotions/' + encodeURIComponent(promotionID)).then(function (body) {
            if (body && body.error && body.error.code === 'forbidden') {
                form.innerHTML = '<p role="alert">Your account does not have products access.</p>';
                return;
            }
            var promotion = body && body.data && body.data.promotion;
            if (!promotion) {
                form.innerHTML = '<p role="alert">' + esc(extractErrorMessage(body, 'Promotion not found.')) + '</p>';
                return;
            }
            bindForm(promotion);
        }).catch(function (err) {
            form.innerHTML = '<p role="alert">' + esc(extractErrorMessage(err, 'Failed to load promotion form.')) + '</p>';
        });
    }

    function renderCouponsGrid(container) {
        container.innerHTML = '<h2>Coupons</h2><div id="coupons-grid"></div>';

        var grid = document.getElementById("coupons-grid");
        api("/admin/coupons?offset=0&limit=50").then(function (body) {
            if (body && body.error && body.error.code === "forbidden") {
                grid.innerHTML = '<p role="alert">Your account does not have products access.</p>';
                return;
            }

            var couponsRaw = body && body.data && body.data.coupons;
            if (!Array.isArray(couponsRaw)) {
                grid.innerHTML = '<p role="alert">' + esc(extractErrorMessage(body, 'Failed to load coupons.')) + '</p>';
                return;
            }

            var html = '<div style="margin-bottom:1rem"><a class="button" href="/admin/marketing/coupons/new" data-link id="new-coupon-btn">New Coupon</a></div>';
            html += '<table><thead><tr>' +
                '<th>Code</th><th>Promotion</th><th>Usage</th><th>Status</th><th>Updated</th>' +
                '</tr></thead><tbody>';

            if (couponsRaw.length === 0) {
                html += '<tr><td colspan="5">No coupons found.</td></tr>';
            } else {
                for (var i = 0; i < couponsRaw.length; i++) {
                    var coupon = couponsRaw[i];
                    var editHref = '/admin/marketing/coupons/' + encodeURIComponent(coupon.id || '');
                    var usageText = String(coupon.usage_count || 0) + ' / ' + (coupon.usage_limit > 0 ? String(coupon.usage_limit) : '∞');
                    html += '<tr>' +
                        '<td><a href="' + editHref + '" data-link>' + esc(coupon.code || coupon.id || '') + '</a></td>' +
                        '<td>' + esc(coupon.promotion_name || coupon.promotion_id || '—') + '</td>' +
                        '<td>' + esc(usageText) + '</td>' +
                        '<td><span class="badge badge-' + esc(coupon.active ? 'active' : 'draft') + '">' + esc(coupon.active ? 'active' : 'inactive') + '</span></td>' +
                        '<td>' + esc(coupon.updated_at ? String(coupon.updated_at).substring(0, 10) : '') + '</td>' +
                        '</tr>';
                }
            }

            html += '</tbody></table>';
            grid.innerHTML = html;
        }).catch(function (err) {
            grid.innerHTML = '<p role="alert">' + esc(extractErrorMessage(err, 'Failed to load coupons.')) + '</p>';
        });
    }

    function renderCouponCreate(container) {
        renderCouponForm(container, null);
    }

    function renderCouponEdit(container, couponID) {
        renderCouponForm(container, couponID);
    }

    function renderCouponFormFields(coupon, promotions) {
        var isActive = coupon ? !!coupon.active : true;
        var selectedPromotionID = coupon ? (coupon.promotion_id || '') : '';
        var promotionOptions = '<option value="">Select promotion</option>';
        for (var i = 0; i < promotions.length; i++) {
            var promo = promotions[i];
            var promoID = promo.id || '';
            promotionOptions += '<option value="' + esc(promoID) + '"' +
                (promoID === selectedPromotionID ? ' selected' : '') + '>' +
                esc(promo.name || promoID) + ' (' + esc(promo.type || '') + ')' +
                '</option>';
        }
        return '' +
            '<label>Code<input name="code" value="' + esc(coupon ? (coupon.code || '') : '') + '" required placeholder="SAVE10"></label>' +
            '<p class="hint">Uppercase letters, digits, and hyphens (min 3 characters).</p>' +
            '<label>Promotion<select name="promotion_id" required>' + promotionOptions + '</select></label>' +
            '<label>Usage limit<input name="usage_limit" type="number" min="0" value="' + esc(coupon ? String(coupon.usage_limit || 0) : '0') + '"></label>' +
            '<p class="hint">0 means unlimited redemptions.</p>' +
            '<label><input type="checkbox" name="active"' + (isActive ? ' checked' : '') + '> Active</label>' +
            '<div style="margin-top:1rem"><button type="submit">Save</button>' +
            (coupon ? ' <button type="button" id="delete-coupon-btn" class="danger">Delete</button>' : '') +
            '</div>';
    }

    function renderCouponForm(container, couponID) {
        var title = couponID ? 'Edit Coupon' : 'New Coupon';
        container.innerHTML =
            '<h2>' + title + '</h2>' +
            '<p><a href="/admin/marketing/coupons" data-link>Back to coupons</a></p>' +
            '<div id="coupon-form-msg"></div>' +
            '<form id="coupon-form"><p>Loading…</p></form>';

        var msg = document.getElementById('coupon-form-msg');
        var form = document.getElementById('coupon-form');

        function bindForm(coupon, promotions) {
            form.innerHTML = renderCouponFormFields(coupon, promotions || []);

            form.addEventListener('submit', function (e) {
                e.preventDefault();
                var usageLimit = parseInt(form.elements.usage_limit.value, 10);
                if (isNaN(usageLimit) || usageLimit < 0) {
                    msg.innerHTML = '<p role="alert">Usage limit must be zero or greater.</p>';
                    return;
                }
                var payload = {
                    code: form.elements.code.value,
                    promotion_id: form.elements.promotion_id.value,
                    usage_limit: usageLimit,
                    active: form.elements.active.checked
                };
                var method = couponID ? 'PUT' : 'POST';
                var url = couponID ? '/admin/coupons/' + encodeURIComponent(couponID) : '/admin/coupons';
                api(url, { method: method, body: JSON.stringify(payload) }).then(function (body) {
                    if (body && body.error) {
                        msg.innerHTML = '<p role="alert">' + esc(body.error.message || 'Save failed.') + '</p>';
                        return;
                    }
                    navigate('/admin/marketing/coupons');
                }).catch(function (err) {
                    msg.innerHTML = '<p role="alert">' + esc(extractErrorMessage(err, 'Save failed.')) + '</p>';
                });
            });

            if (couponID) {
                var deleteBtn = document.getElementById('delete-coupon-btn');
                if (deleteBtn) {
                    deleteBtn.addEventListener('click', function () {
                        if (!window.confirm('Delete this coupon?')) {
                            return;
                        }
                        api('/admin/coupons/' + encodeURIComponent(couponID), { method: 'DELETE' }).then(function (body) {
                            if (body && body.error) {
                                msg.innerHTML = '<p role="alert">' + esc(body.error.message || 'Failed to delete coupon.') + '</p>';
                                return;
                            }
                            navigate('/admin/marketing/coupons');
                        }).catch(function (err) {
                            msg.innerHTML = '<p role="alert">' + esc(extractErrorMessage(err, 'Failed to delete coupon.')) + '</p>';
                        });
                    });
                }
            }
        }

        function loadPromotionsThen(callback) {
            api('/admin/promotions?offset=0&limit=100').then(function (body) {
                if (body && body.error && body.error.code === 'forbidden') {
                    form.innerHTML = '<p role="alert">Your account does not have products access.</p>';
                    return;
                }
                var promotions = body && body.data && body.data.promotions;
                if (!Array.isArray(promotions)) {
                    form.innerHTML = '<p role="alert">' + esc(extractErrorMessage(body, 'Failed to load promotions.')) + '</p>';
                    return;
                }
                callback(promotions);
            }).catch(function (err) {
                form.innerHTML = '<p role="alert">' + esc(extractErrorMessage(err, 'Failed to load promotions.')) + '</p>';
            });
        }

        if (!couponID) {
            loadPromotionsThen(function (promotions) {
                bindForm(null, promotions);
            });
            return;
        }

        Promise.all([
            api('/admin/coupons/' + encodeURIComponent(couponID)),
            api('/admin/promotions?offset=0&limit=100')
        ]).then(function (results) {
            var couponBody = results[0];
            var promotionsBody = results[1];
            if (couponBody && couponBody.error && couponBody.error.code === 'forbidden') {
                form.innerHTML = '<p role="alert">Your account does not have products access.</p>';
                return;
            }
            var coupon = couponBody && couponBody.data && couponBody.data.coupon;
            if (!coupon) {
                form.innerHTML = '<p role="alert">' + esc(extractErrorMessage(couponBody, 'Coupon not found.')) + '</p>';
                return;
            }
            var promotions = promotionsBody && promotionsBody.data && promotionsBody.data.promotions;
            if (!Array.isArray(promotions)) {
                form.innerHTML = '<p role="alert">' + esc(extractErrorMessage(promotionsBody, 'Failed to load promotions.')) + '</p>';
                return;
            }
            bindForm(coupon, promotions);
        }).catch(function (err) {
            form.innerHTML = '<p role="alert">' + esc(extractErrorMessage(err, 'Failed to load coupon form.')) + '</p>';
        });
    }

    function renderAttributesGrid(container) {
        container.innerHTML = '<h2>Attributes</h2><div id="attributes-grid"></div>';

        var grid = document.getElementById("attributes-grid");
        Promise.all([
            api("/admin/attribute-groups"),
            api("/admin/attributes")
        ]).then(function (results) {
            var groupsBody = results[0];
            var attrsBody = results[1];
            if ((groupsBody && groupsBody.error && groupsBody.error.code === "forbidden") ||
                (attrsBody && attrsBody.error && attrsBody.error.code === "forbidden")) {
                grid.innerHTML = '<p role="alert">Your account does not have categories access.</p>';
                return;
            }

            var groups = groupsBody && groupsBody.data && groupsBody.data.groups;
            var attrsRaw = attrsBody && attrsBody.data && attrsBody.data.attributes;
            if (!Array.isArray(groups) || !Array.isArray(attrsRaw)) {
                grid.innerHTML = '<p role="alert">' + esc(extractErrorMessage(attrsBody, 'Failed to load attributes.')) + '</p>';
                return;
            }

            var html = '<div style="margin-bottom:1rem">' +
                '<a class="button" href="/admin/catalog/attributes/new" data-link id="new-attribute-btn">New Attribute</a> ' +
                '<a class="button" href="/admin/catalog/attribute-groups/new" data-link id="new-attribute-group-btn">New Group</a>' +
                '</div>';

            html += '<h3>Attribute Groups</h3><table><thead><tr><th>Code</th><th>Label</th><th>Attributes</th></tr></thead><tbody>';
            if (groups.length === 0) {
                html += '<tr><td colspan="3">No groups defined.</td></tr>';
            } else {
                for (var g = 0; g < groups.length; g++) {
                    var group = groups[g];
                    var groupHref = '/admin/catalog/attribute-groups/' + encodeURIComponent(group.code || '');
                    var memberCodes = Array.isArray(group.attributes) ? group.attributes.join(', ') : '';
                    html += '<tr>' +
                        '<td><a href="' + groupHref + '" data-link>' + esc(group.code || '') + '</a></td>' +
                        '<td>' + esc(group.label || '') + '</td>' +
                        '<td>' + esc(memberCodes) + '</td>' +
                        '</tr>';
                }
            }
            html += '</tbody></table>';

            html += '<h3 style="margin-top:2rem">Attributes</h3>';
            html += '<label>Filter by group <select id="attribute-group-filter"><option value="">All groups</option>';
            for (var gi = 0; gi < groups.length; gi++) {
                html += '<option value="' + esc(groups[gi].code || '') + '">' + esc(groups[gi].label || groups[gi].code || '') + '</option>';
            }
            html += '</select></label>';
            html += '<table style="margin-top:1rem"><thead><tr>' +
                '<th>Code</th><th>Label</th><th>Type</th><th>Required</th><th>Groups</th>' +
                '</tr></thead><tbody id="attributes-table-body">';

            function renderAttributeRows(filterGroup) {
                var rows = '';
                for (var i = 0; i < attrsRaw.length; i++) {
                    var attr = attrsRaw[i];
                    var groupList = Array.isArray(attr.groups) ? attr.groups : [];
                    if (filterGroup && groupList.indexOf(filterGroup) < 0) {
                        continue;
                    }
                    var editHref = '/admin/catalog/attributes/' + encodeURIComponent(attr.code || '');
                    rows += '<tr>' +
                        '<td><a href="' + editHref + '" data-link>' + esc(attr.code || '') + '</a></td>' +
                        '<td>' + esc(attr.label || '') + '</td>' +
                        '<td>' + esc(attr.type || '') + '</td>' +
                        '<td>' + esc(attr.required ? 'yes' : 'no') + '</td>' +
                        '<td>' + esc(groupList.join(', ')) + '</td>' +
                        '</tr>';
                }
                if (rows === '') {
                    rows = '<tr><td colspan="5">No attributes found.</td></tr>';
                }
                return rows;
            }

            html += renderAttributeRows('');
            html += '</tbody></table>';
            grid.innerHTML = html;

            var filterEl = document.getElementById('attribute-group-filter');
            var tbody = document.getElementById('attributes-table-body');
            if (filterEl && tbody) {
                filterEl.addEventListener('change', function () {
                    tbody.innerHTML = renderAttributeRows(filterEl.value);
                });
            }
        }).catch(function (err) {
            grid.innerHTML = '<p role="alert">' + esc(extractErrorMessage(err, 'Failed to load attributes.')) + '</p>';
        });
    }

    function renderAttributeFormFields(attribute) {
        var attrType = attribute ? (attribute.type || 'text') : 'text';
        var isRequired = attribute ? !!attribute.required : false;
        var options = attribute && Array.isArray(attribute.options) ? attribute.options.join(', ') : '';
        return '' +
            (attribute ? '' : '<label>Code<input name="code" value="" required pattern="[a-zA-Z0-9_-]+"></label>') +
            '<label>Label<input name="label" value="' + esc(attribute ? (attribute.label || '') : '') + '" required></label>' +
            '<label>Type<select name="type">' +
            '<option value="text"' + (attrType === 'text' ? ' selected' : '') + '>Text</option>' +
            '<option value="number"' + (attrType === 'number' ? ' selected' : '') + '>Number</option>' +
            '<option value="boolean"' + (attrType === 'boolean' ? ' selected' : '') + '>Boolean</option>' +
            '<option value="select"' + (attrType === 'select' ? ' selected' : '') + '>Select</option>' +
            '</select></label>' +
            '<label>Options (comma-separated, for select)<input name="options" value="' + esc(options) + '"></label>' +
            '<label><input type="checkbox" name="required"' + (isRequired ? ' checked' : '') + '> Required</label>' +
            '<div style="margin-top:1rem"><button type="submit">Save</button>' +
            (attribute ? ' <button type="button" id="delete-attribute-btn" class="danger">Delete</button>' : '') +
            '</div>';
    }

    function parseAttributeOptions(raw) {
        if (!raw) {
            return [];
        }
        return raw.split(',').map(function (part) { return part.trim(); }).filter(function (part) { return part !== ''; });
    }

    function renderAttributeCreate(container) {
        renderAttributeForm(container, null);
    }

    function renderAttributeEdit(container, attributeCode) {
        renderAttributeForm(container, attributeCode);
    }

    function renderAttributeForm(container, attributeCode) {
        var title = attributeCode ? 'Edit Attribute' : 'New Attribute';
        container.innerHTML =
            '<h2>' + title + '</h2>' +
            '<p><a href="/admin/catalog/attributes" data-link>Back to attributes</a></p>' +
            '<div id="attribute-form-msg"></div>' +
            '<form id="attribute-form"><p>Loading…</p></form>';

        var msg = document.getElementById('attribute-form-msg');
        var form = document.getElementById('attribute-form');

        function bindForm(attribute) {
            form.innerHTML = renderAttributeFormFields(attribute);
            form.addEventListener('submit', function (e) {
                e.preventDefault();
                var payload = {
                    label: form.elements.label.value,
                    type: form.elements.type.value,
                    required: form.elements.required.checked,
                    options: parseAttributeOptions(form.elements.options.value)
                };
                if (!attributeCode) {
                    payload.code = form.elements.code.value;
                }
                var method = attributeCode ? 'PUT' : 'POST';
                var url = attributeCode
                    ? '/admin/attributes/' + encodeURIComponent(attributeCode)
                    : '/admin/attributes';
                api(url, { method: method, body: JSON.stringify(payload) }).then(function (body) {
                    if (body && body.error) {
                        msg.innerHTML = '<p role="alert">' + esc(body.error.message || 'Save failed.') + '</p>';
                        return;
                    }
                    navigate('/admin/catalog/attributes');
                }).catch(function (err) {
                    msg.innerHTML = '<p role="alert">' + esc(extractErrorMessage(err, 'Save failed.')) + '</p>';
                });
            });

            if (attributeCode) {
                var deleteBtn = document.getElementById('delete-attribute-btn');
                if (deleteBtn) {
                    deleteBtn.addEventListener('click', function () {
                        if (!window.confirm('Delete this attribute? It will be removed from all groups.')) {
                            return;
                        }
                        api('/admin/attributes/' + encodeURIComponent(attributeCode), { method: 'DELETE' }).then(function (body) {
                            if (body && body.error) {
                                msg.innerHTML = '<p role="alert">' + esc(body.error.message || 'Failed to delete attribute.') + '</p>';
                                return;
                            }
                            navigate('/admin/catalog/attributes');
                        }).catch(function (err) {
                            msg.innerHTML = '<p role="alert">' + esc(extractErrorMessage(err, 'Failed to delete attribute.')) + '</p>';
                        });
                    });
                }
            }
        }

        if (!attributeCode) {
            bindForm(null);
            return;
        }

        api('/admin/attributes/' + encodeURIComponent(attributeCode)).then(function (body) {
            if (body && body.error && body.error.code === 'forbidden') {
                form.innerHTML = '<p role="alert">Your account does not have categories access.</p>';
                return;
            }
            var attribute = body && body.data && body.data.attribute;
            if (!attribute) {
                form.innerHTML = '<p role="alert">' + esc(extractErrorMessage(body, 'Attribute not found.')) + '</p>';
                return;
            }
            bindForm(attribute);
        }).catch(function (err) {
            form.innerHTML = '<p role="alert">' + esc(extractErrorMessage(err, 'Failed to load attribute form.')) + '</p>';
        });
    }

    function renderAttributeGroupCreate(container) {
        renderAttributeGroupForm(container, null);
    }

    function renderAttributeGroupEdit(container, groupCode) {
        renderAttributeGroupForm(container, groupCode);
    }

    function renderAttributeGroupForm(container, groupCode) {
        var title = groupCode ? 'Edit Attribute Group' : 'New Attribute Group';
        container.innerHTML =
            '<h2>' + title + '</h2>' +
            '<p><a href="/admin/catalog/attributes" data-link>Back to attributes</a></p>' +
            '<div id="attribute-group-form-msg"></div>' +
            '<form id="attribute-group-form"><p>Loading…</p></form>';

        var msg = document.getElementById('attribute-group-form-msg');
        var form = document.getElementById('attribute-group-form');

        function bindForm(group, allAttributes) {
            var selected = {};
            if (group && Array.isArray(group.attributes)) {
                for (var i = 0; i < group.attributes.length; i++) {
                    selected[group.attributes[i]] = true;
                }
            }
            var html = (group ? '' : '<label>Code<input name="code" value="" required pattern="[a-zA-Z0-9_-]+"></label>') +
                '<label>Label<input name="label" value="' + esc(group ? (group.label || '') : '') + '" required></label>' +
                '<fieldset><legend>Attributes</legend>';
            if (!Array.isArray(allAttributes) || allAttributes.length === 0) {
                html += '<p>No attributes defined yet.</p>';
            } else {
                for (var a = 0; a < allAttributes.length; a++) {
                    var attr = allAttributes[a];
                    var checked = selected[attr.code] ? ' checked' : '';
                    html += '<label><input type="checkbox" name="attr_' + esc(attr.code) + '" value="' + esc(attr.code) + '"' + checked + '> ' +
                        esc(attr.label || attr.code || '') + ' (' + esc(attr.code || '') + ')</label>';
                }
            }
            html += '</fieldset>' +
                '<div style="margin-top:1rem"><button type="submit">Save</button>' +
                (group ? ' <button type="button" id="delete-attribute-group-btn" class="danger">Delete</button>' : '') +
                '</div>';
            form.innerHTML = html;

            form.addEventListener('submit', function (e) {
                e.preventDefault();
                var attributes = [];
                if (Array.isArray(allAttributes)) {
                    for (var j = 0; j < allAttributes.length; j++) {
                        var code = allAttributes[j].code;
                        var el = form.elements['attr_' + code];
                        if (el && el.checked) {
                            attributes.push(code);
                        }
                    }
                }
                var payload = {
                    label: form.elements.label.value,
                    attributes: attributes
                };
                if (!groupCode) {
                    payload.code = form.elements.code.value;
                }
                var method = groupCode ? 'PUT' : 'POST';
                var url = groupCode
                    ? '/admin/attribute-groups/' + encodeURIComponent(groupCode)
                    : '/admin/attribute-groups';
                api(url, { method: method, body: JSON.stringify(payload) }).then(function (body) {
                    if (body && body.error) {
                        msg.innerHTML = '<p role="alert">' + esc(body.error.message || 'Save failed.') + '</p>';
                        return;
                    }
                    navigate('/admin/catalog/attributes');
                }).catch(function (err) {
                    msg.innerHTML = '<p role="alert">' + esc(extractErrorMessage(err, 'Save failed.')) + '</p>';
                });
            });

            if (groupCode) {
                var deleteBtn = document.getElementById('delete-attribute-group-btn');
                if (deleteBtn) {
                    deleteBtn.addEventListener('click', function () {
                        if (!window.confirm('Delete this attribute group?')) {
                            return;
                        }
                        api('/admin/attribute-groups/' + encodeURIComponent(groupCode), { method: 'DELETE' }).then(function (body) {
                            if (body && body.error) {
                                msg.innerHTML = '<p role="alert">' + esc(body.error.message || 'Failed to delete attribute group.') + '</p>';
                                return;
                            }
                            navigate('/admin/catalog/attributes');
                        }).catch(function (err) {
                            msg.innerHTML = '<p role="alert">' + esc(extractErrorMessage(err, 'Failed to delete attribute group.')) + '</p>';
                        });
                    });
                }
            }
        }

        Promise.all([
            groupCode ? api('/admin/attribute-groups/' + encodeURIComponent(groupCode)) : Promise.resolve(null),
            api('/admin/attributes')
        ]).then(function (results) {
            var groupBody = results[0];
            var attrsBody = results[1];
            if (attrsBody && attrsBody.error && attrsBody.error.code === 'forbidden') {
                form.innerHTML = '<p role="alert">Your account does not have categories access.</p>';
                return;
            }
            var allAttributes = attrsBody && attrsBody.data && attrsBody.data.attributes;
            if (!Array.isArray(allAttributes)) {
                form.innerHTML = '<p role="alert">' + esc(extractErrorMessage(attrsBody, 'Failed to load attributes.')) + '</p>';
                return;
            }
            if (groupCode) {
                if (groupBody && groupBody.error && groupBody.error.code === 'forbidden') {
                    form.innerHTML = '<p role="alert">Your account does not have categories access.</p>';
                    return;
                }
                var group = groupBody && groupBody.data && groupBody.data.group;
                if (!group) {
                    form.innerHTML = '<p role="alert">' + esc(extractErrorMessage(groupBody, 'Attribute group not found.')) + '</p>';
                    return;
                }
                bindForm(group, allAttributes);
                return;
            }
            bindForm(null, allAttributes);
        }).catch(function (err) {
            form.innerHTML = '<p role="alert">' + esc(extractErrorMessage(err, 'Failed to load attribute group form.')) + '</p>';
        });
    }

    function renderInventoryGrid(container) {
        container.innerHTML =
            '<h2>Inventory</h2>' +
            '<form id="inventory-search-form" style="margin-bottom:1rem">' +
            '<label>Search SKU or product <input name="search" placeholder="SKU or product name"></label> ' +
            '<button type="submit">Search</button>' +
            '</form>' +
            '<div id="inventory-grid"></div>';

        var grid = document.getElementById("inventory-grid");
        var searchForm = document.getElementById("inventory-search-form");
        var currentSearch = "";

        function loadInventory() {
            var url = "/admin/inventory?offset=0&limit=50";
            if (currentSearch) {
                url += "&search=" + encodeURIComponent(currentSearch);
            }
            api(url).then(function (body) {
                if (body && body.error && body.error.code === "forbidden") {
                    grid.innerHTML = '<p role="alert">Your account does not have products access.</p>';
                    return;
                }
                var items = body && body.data && body.data.items;
                var threshold = body && body.data && body.data.low_stock_threshold;
                if (!Array.isArray(items)) {
                    grid.innerHTML = '<p role="alert">' + esc(extractErrorMessage(body, 'Failed to load inventory.')) + '</p>';
                    return;
                }

                var html = '<table><thead><tr>' +
                    '<th>SKU</th><th>Product</th><th>Variant</th><th>On hand</th><th>Reserved</th><th>Available</th><th>Status</th><th>Adjust</th>' +
                    '</tr></thead><tbody>';
                if (items.length === 0) {
                    html += '<tr><td colspan="8">No variants found.</td></tr>';
                } else {
                    for (var i = 0; i < items.length; i++) {
                        var item = items[i];
                        var status = item.low_stock
                            ? '<span class="badge badge-draft">low stock</span>'
                            : (item.quantity > 0 ? '<span class="badge badge-active">in stock</span>' : '<span class="badge badge-draft">out of stock</span>');
                        html += '<tr data-variant-id="' + esc(item.variant_id || '') + '">' +
                            '<td>' + esc(item.sku || '') + '</td>' +
                            '<td>' + esc(item.product_name || '') + '</td>' +
                            '<td>' + esc(item.variant_name || '') + '</td>' +
                            '<td>' + esc(String(item.quantity || 0)) + '</td>' +
                            '<td>' + esc(String(item.reserved || 0)) + '</td>' +
                            '<td>' + esc(String(item.available || 0)) + '</td>' +
                            '<td>' + status + '</td>' +
                            '<td><input type="number" min="0" class="inventory-qty-input" value="' + esc(String(item.quantity || 0)) + '" style="width:5rem"> ' +
                            '<button type="button" class="inventory-save-btn">Save</button></td>' +
                            '</tr>';
                    }
                }
                html += '</tbody></table>';
                if (typeof threshold === "number") {
                    html += '<p class="hint">Low stock badge applies when on-hand quantity is below ' + esc(String(threshold)) + '.</p>';
                }
                grid.innerHTML = html;

                var saveButtons = grid.querySelectorAll(".inventory-save-btn");
                for (var j = 0; j < saveButtons.length; j++) {
                    saveButtons[j].addEventListener("click", function (e) {
                        var row = e.target.closest("tr");
                        if (!row) {
                            return;
                        }
                        var variantID = row.getAttribute("data-variant-id");
                        var input = row.querySelector(".inventory-qty-input");
                        if (!variantID || !input) {
                            return;
                        }
                        var qty = parseInt(input.value, 10);
                        if (isNaN(qty) || qty < 0) {
                            window.alert("Quantity must be zero or greater.");
                            return;
                        }
                        api("/admin/inventory/" + encodeURIComponent(variantID), {
                            method: "PUT",
                            body: JSON.stringify({ quantity: qty })
                        }).then(function (resp) {
                            if (resp && resp.error) {
                                window.alert(resp.error.message || "Failed to update stock.");
                                return;
                            }
                            loadInventory();
                        }).catch(function (err) {
                            window.alert(extractErrorMessage(err, "Failed to update stock."));
                        });
                    });
                }
            }).catch(function (err) {
                grid.innerHTML = '<p role="alert">' + esc(extractErrorMessage(err, 'Failed to load inventory.')) + '</p>';
            });
        }

        searchForm.addEventListener("submit", function (e) {
            e.preventDefault();
            currentSearch = searchForm.elements.search.value.trim();
            loadInventory();
        });

        loadInventory();
    }

    function renderPagesGrid(container) {
        container.innerHTML = '<h2>Pages</h2><div id="pages-grid"></div>';

        var grid = document.getElementById("pages-grid");
        api("/admin/pages?offset=0&limit=50").then(function (body) {
            if (body && body.error && body.error.code === "forbidden") {
                grid.innerHTML = '<p role="alert">Your account does not have pages access.</p>';
                return;
            }

            var pagesRaw = body && body.data && body.data.pages;
            if (!Array.isArray(pagesRaw)) {
                grid.innerHTML = '<p role="alert">' + esc(extractErrorMessage(body, 'Failed to load pages.')) + '</p>';
                return;
            }

            var pages = normalizePages(pagesRaw);
            var html = '<div style="margin-bottom:1rem"><a class="button" href="/admin/content/pages/new" data-link id="new-page-btn">New Page</a></div>';
            html += '<table><thead><tr>' +
                '<th>Title</th><th>Slug</th><th>Language</th><th>Status</th><th>Updated</th>' +
                '</tr></thead><tbody>';

            if (pages.length === 0) {
                html += '<tr><td colspan="5">No pages found.</td></tr>';
            } else {
                for (var i = 0; i < pages.length; i++) {
                    var page = pages[i];
                    var editHref = '/admin/content/pages/' + encodeURIComponent(page.id || '');
                    html += '<tr>' +
                        '<td><a href="' + editHref + '" data-link>' + esc(page.title || page.slug || page.id || '') + '</a></td>' +
                        '<td>' + esc(page.slug || '') + '</td>' +
                        '<td>' + esc(page.language || '—') + '</td>' +
                        '<td><span class="badge badge-' + esc(page.is_active ? 'active' : 'draft') + '">' + esc(page.is_active ? 'active' : 'draft') + '</span></td>' +
                        '<td>' + esc(page.updated_at ? String(page.updated_at).substring(0, 10) : '') + '</td>' +
                        '</tr>';
                }
            }

            html += '</tbody></table>';
            grid.innerHTML = html;
        }).catch(function (err) {
            grid.innerHTML = '<p role="alert">' + esc(extractErrorMessage(err, 'Failed to load pages.')) + '</p>';
        });
    }

    function renderPageCreate(container) {
        renderPageForm(container, null);
    }

    function renderPageEdit(container, pageID) {
        renderPageForm(container, pageID);
    }

    function renderPageFormFields(page) {
        var defaultLanguage = page ? (page.language || '') : (adminScope.language || '');
        var isActive = page ? !!page.is_active : true;
        return '' +
            '<label>Title<input name="title" value="' + esc(page ? (page.title || '') : '') + '" required></label>' +
            '<label>Slug<input name="slug" value="' + esc(page ? (page.slug || '') : '') + '" required></label>' +
            '<label>Language<input name="language" value="' + esc(defaultLanguage) + '" placeholder="e.g. en, fr (blank = default)"></label>' +
            '<label>Body<textarea name="content" rows="12">' + esc(page ? (page.content || '') : '') + '</textarea></label>' +
            '<label><input type="checkbox" name="is_active"' + (isActive ? ' checked' : '') + '> Active</label>' +
            '<div style="margin-top:1rem"><button type="submit">Save</button>' +
            (page ? ' <button type="button" id="delete-page-btn" class="danger">Delete</button>' : '') +
            '</div>';
    }

    function renderPageForm(container, pageID) {
        var title = pageID ? 'Edit Page' : 'New Page';
        container.innerHTML =
            '<h2>' + title + '</h2>' +
            '<p><a href="/admin/content/pages" data-link>Back to pages</a></p>' +
            '<div id="page-form-msg"></div>' +
            '<form id="page-form"><p>Loading…</p></form>';

        var msg = document.getElementById('page-form-msg');
        var form = document.getElementById('page-form');

        function bindForm(page) {
            form.innerHTML = renderPageFormFields(page);

            form.addEventListener('submit', function (e) {
                e.preventDefault();
                var payload = {
                    title: form.elements.title.value,
                    slug: form.elements.slug.value,
                    language: form.elements.language.value,
                    content: form.elements.content.value,
                    is_active: form.elements.is_active.checked
                };
                var method = pageID ? 'PUT' : 'POST';
                var url = pageID ? '/admin/pages/' + encodeURIComponent(pageID) : '/admin/pages';
                api(url, { method: method, body: JSON.stringify(payload) }).then(function (body) {
                    if (body && body.error) {
                        msg.innerHTML = '<p role="alert">' + esc(body.error.message || 'Save failed.') + '</p>';
                        return;
                    }
                    msg.innerHTML = '<p>Saved.</p>';
                    var savedPage = normalizePage(body && body.data && body.data.page);
                    if (!pageID && savedPage && savedPage.id) {
                        navigateTo('/admin/content/pages/' + encodeURIComponent(savedPage.id));
                    }
                }).catch(function (err) {
                    msg.innerHTML = '<p role="alert">' + esc(extractErrorMessage(err, 'Save failed.')) + '</p>';
                });
            });

            if (pageID) {
                var deleteBtn = document.getElementById('delete-page-btn');
                if (deleteBtn) {
                    deleteBtn.addEventListener('click', function () {
                        if (!window.confirm('Delete ' + (page.title || page.slug || page.id) + '?')) {
                            return;
                        }
                        api('/admin/pages/' + encodeURIComponent(pageID), { method: 'DELETE' }).then(function (body) {
                            if (body && body.error) {
                                msg.innerHTML = '<p role="alert">' + esc(body.error.message || 'Failed to delete page.') + '</p>';
                                return;
                            }
                            navigateTo('/admin/content/pages');
                        }).catch(function (err) {
                            msg.innerHTML = '<p role="alert">' + esc(extractErrorMessage(err, 'Failed to delete page.')) + '</p>';
                        });
                    });
                }
            }
        }

        if (!pageID) {
            bindForm(null);
            return;
        }

        api('/admin/pages?offset=0&limit=100').then(function (body) {
            if (body && body.error && body.error.code === 'forbidden') {
                msg.innerHTML = '<p role="alert">Your account does not have pages access.</p>';
                form.innerHTML = '';
                return;
            }
            var pagesRaw = body && body.data && body.data.pages;
            if (!Array.isArray(pagesRaw)) {
                msg.innerHTML = '<p role="alert">' + esc(extractErrorMessage(body, 'Failed to load page.')) + '</p>';
                form.innerHTML = '';
                return;
            }
            var pages = normalizePages(pagesRaw);
            var page = null;
            for (var i = 0; i < pages.length; i++) {
                if (pages[i].id === pageID) {
                    page = pages[i];
                    break;
                }
            }
            if (!page) {
                msg.innerHTML = '<p role="alert">Page not found.</p>';
                form.innerHTML = '';
                return;
            }
            bindForm(page);
        }).catch(function (err) {
            msg.innerHTML = '<p role="alert">' + esc(extractErrorMessage(err, 'Failed to load page.')) + '</p>';
            form.innerHTML = '';
        });
    }

    function renderStoresGrid(container) {
        container.innerHTML = '<h2>Stores</h2><div id="stores-grid"></div>';

        var grid = document.getElementById("stores-grid");
        api("/admin/stores").then(function (body) {
            if (body && body.error && body.error.code === "forbidden") {
                grid.innerHTML = '<p role="alert">Your account does not have stores access.</p>';
                return;
            }

            var storesRaw = body && body.data && body.data.stores;
            if (!Array.isArray(storesRaw)) {
                grid.innerHTML = '<p role="alert">' + esc(extractErrorMessage(body, 'Failed to load stores.')) + '</p>';
                return;
            }

            var stores = normalizeStores(storesRaw);
            var html = '<table><thead><tr>' +
                '<th>Name</th><th>Code</th><th>Domain</th><th>Language</th><th>Currency</th><th>Default</th>' +
                '</tr></thead><tbody>';

            if (stores.length === 0) {
                html += '<tr><td colspan="6">No stores found.</td></tr>';
            } else {
                for (var i = 0; i < stores.length; i++) {
                    var store = stores[i];
                    html += '<tr>' +
                        '<td>' + esc(store.name || store.code || store.id || '') + '</td>' +
                        '<td>' + esc(store.code || '') + '</td>' +
                        '<td>' + esc(store.domain || '') + '</td>' +
                        '<td>' + esc(store.language || '') + '</td>' +
                        '<td>' + esc(store.currency || '') + '</td>' +
                        '<td>' + esc(store.is_default ? 'Yes' : 'No') + '</td>' +
                        '</tr>';
                }
            }

            html += '</tbody></table>';
            grid.innerHTML = html;
        }).catch(function (err) {
            grid.innerHTML = '<p role="alert">' + esc(extractErrorMessage(err, 'Failed to load stores.')) + '</p>';
        });
    }

    function renderStoreDomainsPage(container) {
        container.innerHTML = '<h2>Domains</h2><div id="store-domains-grid"></div>';

        var grid = document.getElementById('store-domains-grid');
        api('/admin/stores').then(function (body) {
            if (body && body.error && body.error.code === 'forbidden') {
                grid.innerHTML = '<p role="alert">Your account does not have stores access.</p>';
                return;
            }

            var storesRaw = body && body.data && body.data.stores;
            if (!Array.isArray(storesRaw)) {
                grid.innerHTML = '<p role="alert">' + esc(extractErrorMessage(body, 'Failed to load store domains.')) + '</p>';
                return;
            }

            var stores = normalizeStores(storesRaw);
            var html = '<table><thead><tr>' +
                '<th>Store</th><th>Domain</th><th>Language</th><th>Default</th>' +
                '</tr></thead><tbody>';

            if (stores.length === 0) {
                html += '<tr><td colspan="4">No store domains found.</td></tr>';
            } else {
                for (var i = 0; i < stores.length; i++) {
                    var store = stores[i];
                    html += '<tr>' +
                        '<td>' + esc(store.name || store.code || store.id || '') + '</td>' +
                        '<td>' + esc(store.domain || '') + '</td>' +
                        '<td>' + esc(store.language || '') + '</td>' +
                        '<td>' + esc(store.is_default ? 'Yes' : 'No') + '</td>' +
                        '</tr>';
                }
            }

            html += '</tbody></table>';
            grid.innerHTML = html;
        }).catch(function (err) {
            grid.innerHTML = '<p role="alert">' + esc(extractErrorMessage(err, 'Failed to load store domains.')) + '</p>';
        });
    }

    function renderStoreLanguagesPage(container) {
        container.innerHTML = '<h2>Languages</h2><div id="store-languages-grid"></div>';

        var grid = document.getElementById('store-languages-grid');
        api('/admin/stores').then(function (body) {
            if (body && body.error && body.error.code === 'forbidden') {
                grid.innerHTML = '<p role="alert">Your account does not have stores access.</p>';
                return;
            }

            var storesRaw = body && body.data && body.data.stores;
            if (!Array.isArray(storesRaw)) {
                grid.innerHTML = '<p role="alert">' + esc(extractErrorMessage(body, 'Failed to load store languages.')) + '</p>';
                return;
            }

            var stores = normalizeStores(storesRaw);
            var html = '<table><thead><tr>' +
                '<th>Store</th><th>Language</th><th>Domain</th><th>Default</th>' +
                '</tr></thead><tbody>';

            if (stores.length === 0) {
                html += '<tr><td colspan="4">No store languages found.</td></tr>';
            } else {
                for (var i = 0; i < stores.length; i++) {
                    var store = stores[i];
                    html += '<tr>' +
                        '<td>' + esc(store.name || store.code || store.id || '') + '</td>' +
                        '<td>' + esc(store.language || '') + '</td>' +
                        '<td>' + esc(store.domain || '') + '</td>' +
                        '<td>' + esc(store.is_default ? 'Yes' : 'No') + '</td>' +
                        '</tr>';
                }
            }

            html += '</tbody></table>';
            grid.innerHTML = html;
        }).catch(function (err) {
            grid.innerHTML = '<p role="alert">' + esc(extractErrorMessage(err, 'Failed to load store languages.')) + '</p>';
        });
    }

    function renderStoreCurrenciesPage(container) {
        container.innerHTML = '<h2>Currencies</h2><div id="store-currencies-grid"></div>';

        var grid = document.getElementById('store-currencies-grid');
        api('/admin/stores').then(function (body) {
            if (body && body.error && body.error.code === 'forbidden') {
                grid.innerHTML = '<p role="alert">Your account does not have stores access.</p>';
                return;
            }

            var storesRaw = body && body.data && body.data.stores;
            if (!Array.isArray(storesRaw)) {
                grid.innerHTML = '<p role="alert">' + esc(extractErrorMessage(body, 'Failed to load store currencies.')) + '</p>';
                return;
            }

            var stores = normalizeStores(storesRaw);
            var html = '<table><thead><tr>' +
                '<th>Store</th><th>Currency</th><th>Language</th><th>Domain</th><th>Default</th>' +
                '</tr></thead><tbody>';

            if (stores.length === 0) {
                html += '<tr><td colspan="5">No store currencies found.</td></tr>';
            } else {
                for (var i = 0; i < stores.length; i++) {
                    var store = stores[i];
                    html += '<tr>' +
                        '<td>' + esc(store.name || store.code || store.id || '') + '</td>' +
                        '<td>' + esc(store.currency || '') + '</td>' +
                        '<td>' + esc(store.language || '') + '</td>' +
                        '<td>' + esc(store.domain || '') + '</td>' +
                        '<td>' + esc(store.is_default ? 'Yes' : 'No') + '</td>' +
                        '</tr>';
                }
            }

            html += '</tbody></table>';
            grid.innerHTML = html;
        }).catch(function (err) {
            grid.innerHTML = '<p role="alert">' + esc(extractErrorMessage(err, 'Failed to load store currencies.')) + '</p>';
        });
    }

    function renderCustomerDetail(container, customerID) {
        container.innerHTML =
            '<h2>Customer Detail</h2>' +
            '<p><a href="/admin/customers" data-link>Back to customers</a></p>' +
            '<div id="customer-detail-body">Loading…</div>';

        var bodyBox = document.getElementById("customer-detail-body");
        api("/admin/customers/" + encodeURIComponent(customerID)).then(function (body) {
            if (body && body.error && body.error.code === "forbidden") {
                bodyBox.innerHTML = '<p role="alert">Your account does not have customer access.</p>';
                return;
            }
            if (body && body.error && body.error.code === "not_found") {
                bodyBox.innerHTML = '<p role="alert">Customer not found.</p>';
                return;
            }

            var customer = normalizeCustomer(body && body.data && body.data.customer);
            if (!customer) {
                bodyBox.innerHTML = '<p role="alert">' + esc(extractErrorMessage(body, 'Failed to load customer.')) + '</p>';
                return;
            }

            var fullName = ((customer.first_name || '') + ' ' + (customer.last_name || '')).trim();
            bodyBox.innerHTML = '' +
                '<div class="admin-detail-card">' +
                '<p><strong>Name:</strong> ' + esc(fullName || customer.email || customer.id || '') + '</p>' +
                '<p><strong>Email:</strong> ' + esc(customer.email || '') + '</p>' +
                '<p><strong>Role:</strong> ' + esc(customer.role || '') + '</p>' +
                '<p><strong>Status:</strong> <span class="badge badge-' + esc(customer.status || 'unknown') + '">' + esc(customer.status || 'unknown') + '</span></p>' +
                '<p><strong>Email Verification:</strong> ' + esc(customer.email_verified_at ? 'Verified' : 'Pending') + '</p>' +
                '<p><strong>Created:</strong> ' + esc(customer.created_at ? String(customer.created_at).substring(0, 10) : '') + '</p>' +
                '<p><strong>Updated:</strong> ' + esc(customer.updated_at ? String(customer.updated_at).substring(0, 10) : '') + '</p>' +
                '<p><strong>Customer ID:</strong> ' + esc(customer.id || '') + '</p>' +
                '</div>';
        }).catch(function (err) {
            bodyBox.innerHTML = '<p role="alert">' + esc(extractErrorMessage(err, 'Failed to load customer.')) + '</p>';
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
            '<div id="settings-scope-banner" class="settings-scope-banner"></div>' +
            '<p class="settings-scope-note">Operational configuration has moved to <a href="/admin/operations/shipping" data-link>Shipping</a> and <a href="/admin/operations/payments" data-link>Payments</a>.</p>' +
            '<div class="settings-grid">' +
            '<section><h3>Store Info</h3><div id="settings-store-msg"></div><form id="settings-store-form"></form></section>' +
            '<section><h3>Email</h3><div id="settings-email-msg"></div><form id="settings-email-form"></form></section>' +
            '<section><h3>Media</h3><div id="settings-media-msg"></div><form id="settings-media-form"></form></section>' +
            '</div>';

        Promise.all([
            api('/admin/stores'),
            api('/admin/config?group=store'),
            api('/admin/config?group=email'),
            api('/admin/config?group=media')
        ]).then(function (results) {
            var stores = normalizeStores(results[0] && results[0].data && results[0].data.stores ? results[0].data.stores : []);
            var storePayload = settingsPayloadFromResult(results[1]);
            var emailPayload = settingsPayloadFromResult(results[2]);
            var mediaPayload = settingsPayloadFromResult(results[3]);

            var activeScope = resolveSettingsScope([storePayload, emailPayload, mediaPayload]);
            renderSettingsScopeBanner(stores, activeScope);

            renderStoreSettingsForm(container, choosePrimaryStore(stores), storePayload.entries, storePayload.fieldScopes, activeScope.storeID);
            renderEmailSettingsForm(container, emailPayload.entries, emailPayload.fieldScopes, activeScope.storeID);
            renderMediaSettingsForm(container, mediaPayload.entries, mediaPayload.fieldScopes, activeScope.storeID);
        }).catch(function () {
            container.innerHTML = '<h2>Settings</h2><p role="alert">Failed to load settings.</p>';
        });
    }

    function renderShippingSettingsPage(container) {
        container.innerHTML =
            '<h2>Shipping</h2>' +
            '<div id="shipping-scope-banner" class="settings-scope-banner"></div>' +
            '<div class="settings-grid">' +
            '<section><h3>Tax Rules</h3><div id="settings-tax-msg"></div><form id="settings-tax-form"></form></section>' +
            '<section><h3>EU Price Indication (Omnibus)</h3><div id="settings-legal-msg"></div><form id="settings-legal-form"></form></section>' +
            '</div>';

        Promise.all([
            api('/admin/stores'),
            api('/admin/config?group=tax'),
            api('/admin/config?group=legal')
        ]).then(function (results) {
            var stores = normalizeStores(results[0] && results[0].data && results[0].data.stores ? results[0].data.stores : []);
            var taxPayload = settingsPayloadFromResult(results[1]);
            var legalPayload = settingsPayloadFromResult(results[2]);
            var activeScope = resolveSettingsScope([taxPayload, legalPayload]);

            renderSettingsScopeBanner(stores, activeScope, 'shipping-scope-banner');
            renderTaxSettingsForm(container, taxPayload.entries, taxPayload.fieldScopes, activeScope.storeID);
            renderLegalSettingsForm(container, legalPayload.entries, legalPayload.fieldScopes, activeScope.storeID);
        }).catch(function () {
            container.innerHTML = '<h2>Shipping</h2><p role="alert">Failed to load shipping settings.</p>';
        });
    }

    function renderPaymentSettingsPage(container) {
        container.innerHTML =
            '<h2>Payments</h2>' +
            '<div id="payments-scope-banner" class="settings-scope-banner"></div>' +
            '<div class="settings-grid">' +
            '<section><h3>Currency & Display</h3><div id="settings-currency-msg"></div><form id="settings-currency-form"></form></section>' +
            '</div>';

        Promise.all([
            api('/admin/stores'),
            api('/admin/config?group=currency')
        ]).then(function (results) {
            var stores = normalizeStores(results[0] && results[0].data && results[0].data.stores ? results[0].data.stores : []);
            var currencyPayload = settingsPayloadFromResult(results[1]);
            var activeScope = resolveSettingsScope([currencyPayload]);

            renderSettingsScopeBanner(stores, activeScope, 'payments-scope-banner');
            renderCurrencySettingsForm(container, currencyPayload.entries, currencyPayload.fieldScopes, activeScope.storeID);
        }).catch(function () {
            container.innerHTML = '<h2>Payments</h2><p role="alert">Failed to load payment settings.</p>';
        });
    }

    function renderLocalizationSettingsPage(container) {
        container.innerHTML =
            '<h2>Localization</h2>' +
            '<div id="localization-scope-banner" class="settings-scope-banner"></div>' +
            '<div class="settings-grid">' +
            '<section><h3>Currency & Display</h3><div id="settings-currency-msg"></div><form id="settings-currency-form"></form></section>' +
            '<section><h3>Store Languages</h3><div id="localization-language-grid"></div></section>' +
            '</div>';

        Promise.all([
            api('/admin/stores'),
            api('/admin/config?group=currency')
        ]).then(function (results) {
            var storesResponse = results[0];
            var currencyResponse = results[1];

            if ((storesResponse && storesResponse.error && storesResponse.error.code === 'forbidden') ||
                (currencyResponse && currencyResponse.error && currencyResponse.error.code === 'forbidden')) {
                container.innerHTML = '<h2>Localization</h2><p role="alert">Your account does not have settings access.</p>';
                return;
            }

            var storesRaw = storesResponse && storesResponse.data && storesResponse.data.stores;
            if (!Array.isArray(storesRaw)) {
                container.innerHTML = '<h2>Localization</h2><p role="alert">' + esc(extractErrorMessage(storesResponse, 'Failed to load localization settings.')) + '</p>';
                return;
            }

            if (!currencyResponse || !currencyResponse.data || typeof currencyResponse.data !== 'object') {
                container.innerHTML = '<h2>Localization</h2><p role="alert">' + esc(extractErrorMessage(currencyResponse, 'Failed to load localization settings.')) + '</p>';
                return;
            }

            var stores = normalizeStores(storesRaw);
            var currencyPayload = settingsPayloadFromResult(currencyResponse);
            var activeScope = resolveSettingsScope([currencyPayload]);

            renderSettingsScopeBanner(stores, activeScope, 'localization-scope-banner');
            renderCurrencySettingsForm(container, currencyPayload.entries, currencyPayload.fieldScopes, activeScope.storeID);
            renderStoreLocalizationSummary(stores);
        }).catch(function () {
            container.innerHTML = '<h2>Localization</h2><p role="alert">Failed to load localization settings.</p>';
        });
    }

    function renderIntegrationsPage(container) {
        container.innerHTML = '<h2>Integrations</h2><div id="integrations-grid"></div>';

        var grid = document.getElementById('integrations-grid');
        Promise.all([
            api('/admin/config?group=email'),
            api('/admin/config?group=media'),
            api('/admin/config?group=plugins')
        ]).then(function (results) {
            var emailResponse = results[0];
            var mediaResponse = results[1];
            var pluginsResponse = results[2];

            if ((emailResponse && emailResponse.error && emailResponse.error.code === 'forbidden') ||
                (mediaResponse && mediaResponse.error && mediaResponse.error.code === 'forbidden')) {
                grid.innerHTML = '<p role="alert">Your account does not have settings access.</p>';
                return;
            }

            if (!emailResponse || !emailResponse.data || typeof emailResponse.data !== 'object' ||
                !mediaResponse || !mediaResponse.data || typeof mediaResponse.data !== 'object') {
                grid.innerHTML = '<p role="alert">Failed to load integrations.</p>';
                return;
            }

            var emailPayload = settingsPayloadFromResult(emailResponse);
            var mediaPayload = settingsPayloadFromResult(mediaResponse);
            var pluginsPayload = pluginsResponse && pluginsResponse.data && typeof pluginsResponse.data === 'object'
                ? settingsPayloadFromResult(pluginsResponse)
                : { entries: {}, fieldScopes: {}, fieldDefs: [] };
            if (pluginsResponse && pluginsResponse.data && Array.isArray(pluginsResponse.data.field_defs)) {
                pluginsPayload.fieldDefs = pluginsResponse.data.field_defs;
            }

            var smtpHost = valueOf(emailPayload.entries, 'mail.smtp.host', '');
            var smtpFrom = valueOf(emailPayload.entries, 'mail.smtp.from', '');
            var mediaStorage = valueOf(mediaPayload.entries, 'media.storage', 'local');
            var mediaEndpoint = mediaStorage === 's3'
                ? (valueOf(mediaPayload.entries, 'media.s3.bucket', '') || valueOf(mediaPayload.entries, 'media.s3.base_url', '') || valueOf(mediaPayload.entries, 'media.s3.endpoint', ''))
                : (valueOf(mediaPayload.entries, 'media.local.base_url', '') || valueOf(mediaPayload.entries, 'media.local.base_path', ''));

            var pluginSectionHTML = renderPluginConfigSection(pluginsPayload);

            grid.innerHTML = '' +
                '<div class="settings-grid">' +
                '<section>' +
                '<h3>Email Delivery</h3>' +
                '<p><strong>Status:</strong> ' + esc(smtpHost ? 'Configured' : 'Needs configuration') + '</p>' +
                '<p><strong>SMTP Host:</strong> ' + esc(smtpHost || 'Not set') + '</p>' +
                '<p><strong>From Address:</strong> ' + esc(smtpFrom || 'Not set') + '</p>' +
                '<p><a href="/admin/settings" data-link>Open settings</a></p>' +
                '</section>' +
                '<section>' +
                '<h3>Media Storage</h3>' +
                '<p><strong>Backend:</strong> ' + esc(mediaStorage || 'local') + '</p>' +
                '<p><strong>Endpoint:</strong> ' + esc(mediaEndpoint || 'Not set') + '</p>' +
                '<p><a href="/admin/settings" data-link>Open settings</a></p>' +
                '</section>' +
                '<section>' +
                '<h3>Operational Providers</h3>' +
                '<p>Shipping and payment provider configuration is managed under Operations.</p>' +
                '<p><a href="/admin/operations/shipping" data-link>Shipping</a> and <a href="/admin/operations/payments" data-link>Payments</a></p>' +
                '</section>' +
                pluginSectionHTML +
                '</div>';
        }).catch(function () {
            grid.innerHTML = '<p role="alert">Failed to load integrations.</p>';
        });
    }

    function renderPluginConfigSection(pluginsPayload) {
        var defs = pluginsPayload.fieldDefs || [];
        if (!defs.length) {
            return '<section>' +
                '<h3>Plugin Extensions</h3>' +
                '<p>Plugin integrations are registered at application boot. No plugin settings are exposed in admin for the current configuration.</p>' +
                '</section>';
        }

        var fieldsHTML = '';
        for (var i = 0; i < defs.length; i++) {
            var field = defs[i];
            var value = valueOf(pluginsPayload.entries, field.key, '');
            var inputType = field.type === 'int' ? 'number' : (field.type === 'bool' ? 'checkbox' : 'text');
            var inputHTML;
            if (field.type === 'bool') {
                inputHTML = '<input type="checkbox" id="plugin-' + esc(field.key) + '" data-plugin-key="' + esc(field.key) + '"' + (value ? ' checked' : '') + '>';
            } else {
                inputHTML = '<input type="' + esc(inputType) + '" id="plugin-' + esc(field.key) + '" data-plugin-key="' + esc(field.key) + '" value="' + esc(value) + '">';
            }
            fieldsHTML += '<label for="plugin-' + esc(field.key) + '"><strong>' + esc(field.label || field.key) + '</strong></label>';
            if (field.description) {
                fieldsHTML += '<p class="hint">' + esc(field.description) + '</p>';
            }
            fieldsHTML += inputHTML;
            fieldsHTML += '<p class="hint">Plugin: ' + esc(field.plugin || '') + '</p>';
        }

        return '<section>' +
            '<h3>Plugin Settings</h3>' +
            '<form id="integrations-plugin-form" class="settings-form">' +
            fieldsHTML +
            '<button type="submit">Save plugin settings</button>' +
            '<div id="integrations-plugin-msg"></div>' +
            '</form>' +
            '</section>';
    }

    document.addEventListener('submit', function (evt) {
        var form = evt.target;
        if (!form || form.id !== 'integrations-plugin-form') {
            return;
        }
        evt.preventDefault();
        var entries = {};
        var inputs = form.querySelectorAll('[data-plugin-key]');
        for (var i = 0; i < inputs.length; i++) {
            var input = inputs[i];
            var key = input.getAttribute('data-plugin-key');
            if (!key) {
                continue;
            }
            if (input.type === 'checkbox') {
                entries[key] = input.checked;
            } else if (input.type === 'number') {
                entries[key] = parseInt(input.value, 10) || 0;
            } else {
                entries[key] = input.value;
            }
        }
        saveSettingsEntries('integrations-plugin-msg', entries);
    }, true);

    function settingsPayloadFromResult(result) {
        var data = result && result.data ? result.data : {};
        var scope = data.scope || {};
        return {
            entries: data.entries || {},
            fieldScopes: data.field_scopes || {},
            storeID: scope.store_id || '',
            language: scope.language || '',
            currency: scope.currency || ''
        };
    }

    function resolveSettingsScope(payloads) {
        var out = {
            storeID: '',
            language: '',
            currency: ''
        };
        for (var i = 0; i < payloads.length; i++) {
            if (!payloads[i]) {
                continue;
            }
            if (!out.storeID && payloads[i].storeID) {
                out.storeID = payloads[i].storeID;
            }
            if (!out.language && payloads[i].language) {
                out.language = payloads[i].language;
            }
            if (!out.currency && payloads[i].currency) {
                out.currency = payloads[i].currency;
            }
        }
        if (!out.storeID) {
            out.storeID = adminScope.store_id || '';
        }
        if (!out.language) {
            out.language = adminScope.language || '';
        }
        if (!out.currency) {
            out.currency = adminScope.currency || '';
        }
        return out;
    }

    function renderSettingsScopeBanner(stores, scope, bannerID) {
        var targetID = bannerID || 'settings-scope-banner';
        var banner = document.getElementById(targetID);
        if (!banner) {
            return;
        }
        var storeID = scope && scope.storeID ? scope.storeID : '';
        var language = scope && scope.language ? scope.language : '';
        var currency = scope && scope.currency ? scope.currency : '';
        var storeName = '';
        for (var i = 0; i < stores.length; i++) {
            if (stores[i] && stores[i].id === storeID) {
                storeName = stores[i].name || stores[i].code || stores[i].id;
                break;
            }
        }
        var contextMeta = '';
        if (language) {
            contextMeta += ' Language: <strong>' + esc(language) + '</strong>.';
        }
        if (currency) {
            contextMeta += ' Currency: <strong>' + esc(currency) + '</strong>.';
        }
        if (storeID) {
            banner.innerHTML = '<p><strong>Current settings scope:</strong> Store override for <strong>' + esc(storeName || storeID) + '</strong>.' + contextMeta + ' Change store in the header switcher to edit another store override.</p>';
            return;
        }
        banner.innerHTML = '<p><strong>Current settings scope:</strong> Global defaults.' + contextMeta + ' Select a store in the header switcher to edit store-specific overrides.</p>';
    }

    function fieldScopeType(fieldScopes, key) {
        if (fieldScopes && fieldScopes[key]) {
            return fieldScopes[key];
        }
        return 'global';
    }

    function renderFieldScopeBadge(fieldScopes, key) {
        var scope = fieldScopeType(fieldScopes, key);
        if (scope === 'store') {
            return ' <span class="settings-scope-badge settings-scope-badge-store">Store-scoped</span>';
        }
        if (scope === 'translatable') {
            return ' <span class="settings-scope-badge settings-scope-badge-translatable">Translatable</span>';
        }
        return ' <span class="settings-scope-badge settings-scope-badge-global">Global</span>';
    }

    function renderFormScopeNote(fieldScopes, keys, storeID) {
        var hasStoreScoped = false;
        for (var i = 0; i < keys.length; i++) {
            if (fieldScopeType(fieldScopes, keys[i]) === 'store') {
                hasStoreScoped = true;
                break;
            }
        }
        if (hasStoreScoped) {
            if (storeID) {
                return '<p class="settings-scope-note">Edits to store-scoped fields save as overrides for the current store context.</p>';
            }
            return '<p class="settings-scope-note">Store-scoped fields currently save as global defaults because no store context is active.</p>';
        }
        return '<p class="settings-scope-note">These fields are global and apply across stores.</p>';
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

    function renderStoreSettingsForm(container, store, storeSettings, fieldScopes, activeStoreID) {
        var form = document.getElementById('settings-store-form');
        form.innerHTML = '' +
            renderFormScopeNote(fieldScopes, ['store.address', 'store.logo'], activeStoreID) +
            '<label>Code<input name="code" value="' + esc(store ? store.code : '') + '" required></label>' +
            '<label>Name<input name="name" value="' + esc(store ? store.name : '') + '" required></label>' +
            '<label>Domain / URL<input name="domain" value="' + esc(store ? store.domain : '') + '"></label>' +
            '<label>Country<input name="country" value="' + esc(store ? store.country : '') + '" required></label>' +
            '<label>Language<input name="language" value="' + esc(store ? store.language : '') + '" required></label>' +
            '<label>Currency<input name="currency" value="' + esc(store ? store.currency : '') + '" required></label>' +
            '<label>Address' + renderFieldScopeBadge(fieldScopes, 'store.address') + '<textarea name="store_address">' + esc(valueOf(storeSettings, 'store.address', '')) + '</textarea></label>' +
            '<label>Logo URL' + renderFieldScopeBadge(fieldScopes, 'store.logo') + '<input name="store_logo" value="' + esc(valueOf(storeSettings, 'store.logo', '')) + '"></label>' +
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

    function renderStoreLocalizationSummary(stores) {
        var grid = document.getElementById('localization-language-grid');
        if (!grid) {
            return;
        }

        var html = '<p class="settings-scope-note">Review the assigned language and default currency for each store.</p>' +
            '<table><thead><tr><th>Store</th><th>Language</th><th>Currency</th><th>Domain</th></tr></thead><tbody>';

        if (!stores || stores.length === 0) {
            html += '<tr><td colspan="4">No stores found.</td></tr>';
        } else {
            for (var i = 0; i < stores.length; i++) {
                var store = stores[i];
                html += '<tr>' +
                    '<td>' + esc(store.name || store.code || store.id || '') + '</td>' +
                    '<td>' + esc(store.language || '') + '</td>' +
                    '<td>' + esc(store.currency || '') + '</td>' +
                    '<td>' + esc(store.domain || '') + '</td>' +
                    '</tr>';
            }
        }

        html += '</tbody></table>';
        grid.innerHTML = html;
    }

    function renderEmailSettingsForm(container, settings, fieldScopes, activeStoreID) {
        var form = document.getElementById('settings-email-form');
        var passwordValue = valueOf(settings, 'mail.smtp.password', '');
        form.innerHTML = '' +
            renderFormScopeNote(fieldScopes, ['mail.smtp.host', 'mail.smtp.port', 'mail.smtp.user', 'mail.smtp.password', 'mail.smtp.from'], activeStoreID) +
            '<label>SMTP Host' + renderFieldScopeBadge(fieldScopes, 'mail.smtp.host') + '<input name="host" value="' + esc(valueOf(settings, 'mail.smtp.host', '')) + '"></label>' +
            '<label>SMTP Port' + renderFieldScopeBadge(fieldScopes, 'mail.smtp.port') + '<input name="port" type="number" min="1" value="' + esc(String(valueOf(settings, 'mail.smtp.port', 0) || '')) + '"></label>' +
            '<label>SMTP User' + renderFieldScopeBadge(fieldScopes, 'mail.smtp.user') + '<input name="user" value="' + esc(valueOf(settings, 'mail.smtp.user', '')) + '"></label>' +
            '<label>SMTP Password' + renderFieldScopeBadge(fieldScopes, 'mail.smtp.password') + '<input name="password" type="password" value="' + esc(passwordValue) + '"></label>' +
            '<label>From Address' + renderFieldScopeBadge(fieldScopes, 'mail.smtp.from') + '<input name="from" value="' + esc(valueOf(settings, 'mail.smtp.from', '')) + '"></label>' +
            '<label>Test Recipient<input name="test_to" type="email" placeholder="merchant@example.com"></label>' +
            '<div class="settings-actions">' +
            '<button type="submit">Save Email Settings</button>' +
            '<button type="button" id="settings-test-email" class="secondary">Send Test Email</button>' +
            '</div>';

        form.addEventListener('submit', function (e) {
            e.preventDefault();
            var entries = {
                'mail.smtp.host': form.elements.host.value,
                'mail.smtp.port': Number(form.elements.port.value || 0),
                'mail.smtp.user': form.elements.user.value,
                'mail.smtp.from': form.elements.from.value
            };
            if (form.elements.password.value !== '***') {
                entries['mail.smtp.password'] = form.elements.password.value;
            }
            saveSettingsEntries('settings-email-msg', entries);
        });

        document.getElementById('settings-test-email').addEventListener('click', function () {
            var payload = {
                to: form.elements.test_to.value,
                host: form.elements.host.value,
                port: Number(form.elements.port.value || 0),
                user: form.elements.user.value,
                from: form.elements.from.value
            };
            if (form.elements.password.value !== '***') {
                payload.password = form.elements.password.value;
            }
            api('/admin/config/test-email', {
                method: 'POST',
                body: JSON.stringify(payload)
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

    function renderMediaSettingsForm(container, settings, fieldScopes, activeStoreID) {
        var form = document.getElementById('settings-media-form');
        form.innerHTML = '' +
            renderFormScopeNote(fieldScopes, ['media.storage', 'media.local.base_path', 'media.local.base_url', 'media.s3.endpoint', 'media.s3.bucket', 'media.s3.region', 'media.s3.base_url', 'media.s3.public_acl'], activeStoreID) +
            '<label>Storage' + renderFieldScopeBadge(fieldScopes, 'media.storage') + '<select name="storage">' +
            renderSelectOptions(['local', 's3'], valueOf(settings, 'media.storage', 'local')) +
            '</select></label>' +
            '<label>Local Base Path' + renderFieldScopeBadge(fieldScopes, 'media.local.base_path') + '<input name="local_base_path" value="' + esc(valueOf(settings, 'media.local.base_path', '')) + '"></label>' +
            '<label>Local Base URL' + renderFieldScopeBadge(fieldScopes, 'media.local.base_url') + '<input name="local_base_url" value="' + esc(valueOf(settings, 'media.local.base_url', '')) + '"></label>' +
            '<label>S3 Endpoint' + renderFieldScopeBadge(fieldScopes, 'media.s3.endpoint') + '<input name="s3_endpoint" value="' + esc(valueOf(settings, 'media.s3.endpoint', '')) + '"></label>' +
            '<label>S3 Bucket' + renderFieldScopeBadge(fieldScopes, 'media.s3.bucket') + '<input name="s3_bucket" value="' + esc(valueOf(settings, 'media.s3.bucket', '')) + '"></label>' +
            '<label>S3 Region' + renderFieldScopeBadge(fieldScopes, 'media.s3.region') + '<input name="s3_region" value="' + esc(valueOf(settings, 'media.s3.region', '')) + '"></label>' +
            '<label>S3 Base URL' + renderFieldScopeBadge(fieldScopes, 'media.s3.base_url') + '<input name="s3_base_url" value="' + esc(valueOf(settings, 'media.s3.base_url', '')) + '"></label>' +
            '<label><input type="checkbox" name="s3_public_acl" ' + (truthy(valueOf(settings, 'media.s3.public_acl', false)) ? 'checked' : '') + '> S3 Public ACL' + renderFieldScopeBadge(fieldScopes, 'media.s3.public_acl') + '</label>' +
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

    function renderCurrencySettingsForm(container, settings, fieldScopes, activeStoreID) {
        var form = document.getElementById('settings-currency-form');
        form.innerHTML = '' +
            renderFormScopeNote(fieldScopes, ['default_currency', 'currency.display_format'], activeStoreID) +
            '<label>Default Currency' + renderFieldScopeBadge(fieldScopes, 'default_currency') + '<input name="default_currency" value="' + esc(valueOf(settings, 'default_currency', 'EUR')) + '"></label>' +
            '<label>Display Format' + renderFieldScopeBadge(fieldScopes, 'currency.display_format') + '<input name="display_format" value="' + esc(valueOf(settings, 'currency.display_format', '{currency} {amount}')) + '"></label>' +
            '<small class="admin-form-hint">Use {currency} and {amount} placeholders, for example "{currency} {amount}" or "{amount} {currency}".</small>' +
            '<button type="submit">Save Currency Settings</button>';
        form.addEventListener('submit', function (e) {
            e.preventDefault();
            saveSettingsEntries('settings-currency-msg', {
                'default_currency': form.elements.default_currency.value,
                'currency.display_format': form.elements.display_format.value
            });
        });
    }

    function renderTaxSettingsForm(container, settings, fieldScopes, activeStoreID) {
        var form = document.getElementById('settings-tax-form');
        form.innerHTML = '' +
            renderFormScopeNote(fieldScopes, ['tax.default_class', 'tax.included'], activeStoreID) +
            '<label>Default Tax Class' + renderFieldScopeBadge(fieldScopes, 'tax.default_class') + '<input name="default_class" value="' + esc(valueOf(settings, 'tax.default_class', 'standard')) + '"></label>' +
            '<label><input type="checkbox" name="tax_included" ' + (truthy(valueOf(settings, 'tax.included', false)) ? 'checked' : '') + '> Prices Include Tax' + renderFieldScopeBadge(fieldScopes, 'tax.included') + '</label>' +
            '<button type="submit">Save Tax Settings</button>';
        form.addEventListener('submit', function (e) {
            e.preventDefault();
            saveSettingsEntries('settings-tax-msg', {
                'tax.default_class': form.elements.default_class.value,
                'tax.included': !!form.elements.tax_included.checked
            });
        });
    }

    function renderLegalSettingsForm(container, settings, fieldScopes, activeStoreID) {
        var form = document.getElementById('settings-legal-form');
        form.innerHTML = '' +
            renderFormScopeNote(fieldScopes, ['legal.omnibus_enabled'], activeStoreID) +
            '<label><input type="checkbox" name="omnibus_enabled" ' + (truthy(valueOf(settings, 'legal.omnibus_enabled', true)) ? 'checked' : '') + '> Show lowest price in last 30 days on discounted products (EU Omnibus)' + renderFieldScopeBadge(fieldScopes, 'legal.omnibus_enabled') + '</label>' +
            '<small class="admin-form-hint">When enabled, product and listing pages show the lowest prior price when the current price is reduced.</small>' +
            '<button type="submit">Save Legal Settings</button>';
        form.addEventListener('submit', function (e) {
            e.preventDefault();
            saveSettingsEntries('settings-legal-msg', {
                'legal.omnibus_enabled': !!form.elements.omnibus_enabled.checked
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
        adminScopeStores = [];
        renderContextSwitcher();
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
        bindContextSwitcher();

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
        loadCurrentUser().then(function () {
            return loadContextSwitcherData();
        }).then(handleRoute);
    }

    if (document.readyState === "loading") {
        document.addEventListener("DOMContentLoaded", init);
    } else {
        init();
    }
})();
