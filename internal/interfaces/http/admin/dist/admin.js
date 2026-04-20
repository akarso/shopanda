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
        "/admin/products": { title: "Products", render: renderPlaceholder("Products"), auth: true },
        "/admin/orders": { title: "Orders", render: renderPlaceholder("Orders"), auth: true },
        "/admin/media": { title: "Media", render: renderPlaceholder("Media"), auth: true },
        "/admin/settings": { title: "Settings", render: renderPlaceholder("Settings"), auth: true }
    };

    function navigateTo(path) {
        history.pushState(null, "", path);
        handleRoute();
    }

    function handleRoute() {
        var path = location.pathname;
        var route = routes[path];
        if (!route) {
            route = routes["/admin/dashboard"];
            if (!route) return;
        }

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
            if (href === currentPath) {
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
        container.innerHTML = "<h2>Dashboard</h2><p>Overview will be available in a future update.</p>";
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
