"use strict";

const { test } = require("node:test");
const assert = require("node:assert/strict");
const ui = require("./dist/product_composition_section.js");

function fakeEl(props) {
    const el = Object.assign({
        value: "",
        checked: false,
        disabled: false,
        listeners: {}
    }, props || {});
    el.addEventListener = function (type, fn) {
        el.listeners[type] = el.listeners[type] || [];
        el.listeners[type].push(fn);
    };
    el.dispatch = function (type) {
        (el.listeners[type] || []).forEach(function (fn) { fn(); });
    };
    return el;
}

function fakeRoot(map) {
    return {
        querySelector(sel) {
            return map[sel] || null;
        }
    };
}

test("modesFromProduct reads normalized and PascalCase fields", () => {
    assert.deepEqual(ui.modesFromProduct({
        pricingMode: "dynamic",
        stockMode: "fixed",
        explodeOrderLines: true
    }), {
        pricing_mode: "dynamic",
        stock_mode: "fixed",
        explode_order_lines: true
    });
    assert.deepEqual(ui.modesFromProduct({
        PricingMode: "dynamic",
        StockMode: "dynamic",
        ExplodeOrderLines: true
    }).explode_order_lines, true);
    assert.deepEqual(ui.modesFromProduct(null), {
        pricing_mode: "fixed",
        stock_mode: "fixed",
        explode_order_lines: false
    });
});

test("collectModes sends three fields only for bundle", () => {
    const pricing = fakeEl({ value: "dynamic" });
    const stock = fakeEl({ value: "dynamic" });
    const explode = fakeEl({ checked: true });
    const root = fakeRoot({
        "#product-bundle-pricing-mode": pricing,
        "#product-bundle-stock-mode": stock,
        "#product-bundle-explode-order-lines": explode
    });
    assert.equal(ui.collectModes(root, "simple"), null);
    assert.equal(ui.collectModes(root, "grouped"), null);
    assert.deepEqual(ui.collectModes(root, "bundle"), {
        pricing_mode: "dynamic",
        stock_mode: "dynamic",
        explode_order_lines: true
    });
});

test("explode checkbox is enabled only when both modes are dynamic", () => {
    const pricing = fakeEl({ value: "dynamic" });
    const stock = fakeEl({ value: "fixed" });
    const explode = fakeEl({ checked: true });
    ui.syncExplodeControl(pricing, stock, explode);
    assert.equal(explode.disabled, true);
    assert.equal(explode.checked, false);
    stock.value = "dynamic";
    ui.syncExplodeControl(pricing, stock, explode);
    assert.equal(explode.disabled, false);

    const root = fakeRoot({
        "#product-bundle-pricing-mode": pricing,
        "#product-bundle-stock-mode": stock,
        "#product-bundle-explode-order-lines": explode
    });
    explode.checked = true;
    ui.bindBundleModes(root);
    pricing.value = "fixed";
    pricing.dispatch("change");
    assert.equal(explode.checked, false);
    assert.equal(explode.disabled, true);
    assert.equal(ui.collectModes(root, "bundle").explode_order_lines, false);
});

test("mergeLeafProducts uses three typed lists and excludes the parent", () => {
    const products = ui.mergeLeafProducts({
        simple: [
            { ID: "parent", Name: "Parent", Slug: "parent", Type: "simple" },
            { ID: "leaf", Name: "Leaf", Slug: "leaf", Type: "simple" }
        ],
        virtual: [{ id: "virt", name: "Virt", slug: "virt", type: "virtual" }],
        downloadable: [{ ID: "file", Name: "File", Slug: "file", Type: "downloadable" }],
        bundle: [{ ID: "kit", Name: "Kit", Slug: "kit", Type: "bundle" }]
    }, "parent");
    const ids = products.map(function (p) { return p.id; });
    assert.deepEqual(ids, ["leaf", "virt", "file"]);
});

test("bundle picker renders quantity and required; grouped picker does not", () => {
    const products = ui.mergeLeafProducts({
        simple: [{ ID: "c1", Name: "Child", Slug: "child", Type: "simple" }],
        virtual: [],
        downloadable: []
    }, "parent");
    const bundleHTML = ui.renderBundlePickerBody({
        products: products,
        assigned: { c1: { quantity: 2, required: false } },
        search: "",
        canWrite: true
    });
    assert.match(bundleHTML, /data-bundle-qty="c1"/);
    assert.match(bundleHTML, /value="2"/);
    assert.match(bundleHTML, /data-bundle-required="c1"/);
    assert.doesNotMatch(bundleHTML, /data-bundle-required="c1" checked/);
    const groupedHTML = ui.renderGroupedPickerBody({
        products: products,
        assigned: { c1: true },
        search: "",
        canWrite: true
    });
    assert.match(groupedHTML, /data-grouped-toggle="c1" checked/);
    assert.doesNotMatch(groupedHTML, /data-bundle-qty/);
});

test("assignment requests match the admin routes", () => {
    assert.deepEqual(ui.bundleAssignmentRequest("b1", "c1", true, "3", false), {
        method: "POST",
        path: "/admin/products/b1/bundle-components",
        body: { component_product_id: "c1", quantity: 3, required: false }
    });
    assert.deepEqual(ui.bundleAssignmentRequest("b1", "c1", false, "1", true), {
        method: "DELETE",
        path: "/admin/products/b1/bundle-components/c1",
        body: null
    });
    assert.deepEqual(ui.groupedAssignmentRequest("g1", "m1", true), {
        method: "POST",
        path: "/admin/products/g1/grouped-members",
        body: { member_product_id: "m1" }
    });
    const html = ui.renderBundleModesHTML({ pricing_mode: "fixed", stock_mode: "fixed", explode_order_lines: true });
    assert.match(html, /id="product-bundle-explode-order-lines"[^>]*disabled/);
    assert.doesNotMatch(html, /id="product-bundle-explode-order-lines" checked/);
    const createHTML = ui.renderBundleSectionHTML({ pricing_mode: "fixed", stock_mode: "fixed", explode_order_lines: false }, false);
    assert.match(createHTML, /Save the product to add bundle components/);
    assert.doesNotMatch(createHTML, /data-bundle-picker/);
    const editHTML = ui.renderBundleSectionHTML({ pricing_mode: "dynamic", stock_mode: "dynamic", explode_order_lines: true }, true);
    assert.match(editHTML, /data-bundle-picker/);
    assert.match(ui.renderGroupedSectionHTML(false), /Save the product to add grouped members/);
});

test("loadLeafProducts calls three typed lists", async () => {
    const calls = [];
    const api = async function (url) {
        calls.push(url);
        const type = new URL(url, "http://local").searchParams.get("type");
        return { data: { products: [{ ID: type + "-1", Name: type, Slug: type, Type: type }] } };
    };
    const products = await ui.loadLeafProducts(api, "simple-1");
    assert.deepEqual(calls, [
        "/admin/products?type=simple&offset=0&limit=100",
        "/admin/products?type=virtual&offset=0&limit=100",
        "/admin/products?type=downloadable&offset=0&limit=100"
    ]);
    assert.deepEqual(products.map(function (p) { return p.id; }), ["virtual-1", "downloadable-1"]);
});

function fullPage(prefix) {
    const products = [];
    for (let i = 0; i < 100; i++) {
        const id = prefix + i;
        products.push({ ID: id, Name: id, Slug: id, Type: "simple" });
    }
    return products;
}

test("loadLeafProducts loads one page per type and load more fetches the next offset", async () => {
    const calls = [];
    const api = async function (url) {
        calls.push(url);
        const parsed = new URL(url, "http://local");
        const type = parsed.searchParams.get("type");
        const offset = Number(parsed.searchParams.get("offset") || "0");
        if (type !== "simple") {
            return { data: { products: [] } };
        }
        if (offset === 0) {
            return { data: { products: fullPage("s") } };
        }
        if (offset === 100) {
            return { data: { products: [{ ID: "s100", Name: "Past", Slug: "past", Type: "simple" }] } };
        }
        return { data: { products: fullPage("x") } };
    };
    const cache = ui.createLeafCache();
    const first = await ui.loadLeafProducts(api, "parent", cache);
    assert.equal(first.length, 100);
    assert.equal(first.some(function (p) { return p.id === "s100"; }), false);
    assert.equal(calls.filter(function (url) { return url.indexOf("offset=100") !== -1; }).length, 0);
    assert.equal(calls.length, 3);
    const second = await ui.loadLeafProducts(api, "parent", cache);
    assert.ok(second.some(function (p) { return p.id === "s100"; }));
    assert.equal(second.length, 101);
    assert.ok(calls.indexOf("/admin/products?type=simple&offset=100&limit=100") !== -1);
    assert.equal(calls.filter(function (url) { return url.indexOf("type=virtual") !== -1; }).length, 1);
});

test("loadLeafType stops after LEAF_MAX_PAGES when every page is full", async () => {
    let calls = 0;
    const api = async function () {
        calls++;
        return { data: { products: fullPage("p" + calls + "-") } };
    };
    const products = await ui.loadLeafType(api, "simple", 0, []);
    assert.equal(calls, ui.LEAF_MAX_PAGES);
    assert.equal(products.length, ui.LEAF_MAX_PAGES * ui.LEAF_PAGE_LIMIT);
});

test("loadLeafProducts does not request past the max page cap", async () => {
    const cache = ui.createLeafCache();
    cache.done.virtual = true;
    cache.done.downloadable = true;
    let calls = 0;
    const api = async function () {
        calls++;
        return { data: { products: fullPage("m" + calls + "-") } };
    };
    for (let i = 0; i < ui.LEAF_MAX_PAGES + 3; i++) {
        await ui.loadLeafProducts(api, "parent", cache);
    }
    assert.equal(calls, ui.LEAF_MAX_PAGES);
});

test("productsWithAssigned keeps one row when the same member is listed twice", () => {
    const merged = ui.productsWithAssigned(
        [{ id: "leaf", name: "Leaf", slug: "leaf", type: "simple" }],
        "parent",
        [
            { member_product_id: "orphan", name: "Orphan", slug: "orphan", type: "virtual" },
            { member_product_id: "orphan", name: "Orphan", slug: "orphan", type: "virtual" }
        ],
        function (row) { return row.member_product_id; }
    );
    assert.deepEqual(merged.products.map(function (p) { return p.id; }), ["leaf", "orphan"]);
    assert.equal(merged.assigned.length, 2);
});

function installPickerDOM() {
    function attrsFrom(raw) {
        const attrs = {};
        const re = /([^\s=/]+)(?:="([^"]*)")?/g;
        let match;
        while ((match = re.exec(raw))) {
            if (match[1] === "/") {
                continue;
            }
            attrs[match[1]] = match[2] == null ? "" : match[2];
        }
        return attrs;
    }

    function El(tag) {
        this.tagName = String(tag || "div").toUpperCase();
        this.children = [];
        this.attributes = {};
        this.listeners = {};
        this.parentNode = null;
        this.value = "";
        this.checked = false;
        this.disabled = false;
    }
    El.prototype.setAttribute = function (name, value) {
        this.attributes[name] = String(value);
        if (name === "value") {
            this.value = String(value);
        }
        if (name === "checked") {
            this.checked = true;
        }
        if (name === "disabled") {
            this.disabled = true;
        }
    };
    El.prototype.getAttribute = function (name) {
        return Object.prototype.hasOwnProperty.call(this.attributes, name) ? this.attributes[name] : null;
    };
    El.prototype.appendChild = function (child) {
        child.parentNode = this;
        this.children.push(child);
        return child;
    };
    El.prototype.addEventListener = function (type, fn) {
        this.listeners[type] = this.listeners[type] || [];
        this.listeners[type].push(fn);
    };
    El.prototype.dispatchEvent = function (event) {
        (this.listeners[event.type] || []).forEach(function (fn) { fn(event); });
    };
    El.prototype.focus = function () {
        document.activeElement = this;
    };
    function matches(el, sel) {
        if (sel.charAt(0) !== "[") {
            return false;
        }
        const body = sel.slice(1, -1);
        const eq = body.indexOf("=");
        if (eq === -1) {
            return el.getAttribute(body) != null;
        }
        let value = body.slice(eq + 1);
        if (value.charAt(0) === '"' && value.charAt(value.length - 1) === '"') {
            value = value.slice(1, -1);
        }
        return el.getAttribute(body.slice(0, eq)) === value;
    }
    function walk(el, sel, out) {
        for (let i = 0; i < el.children.length; i++) {
            const child = el.children[i];
            if (matches(child, sel)) {
                out.push(child);
            }
            walk(child, sel, out);
        }
    }
    El.prototype.querySelectorAll = function (sel) {
        const out = [];
        walk(this, sel, out);
        return out;
    };
    El.prototype.querySelector = function (sel) {
        return this.querySelectorAll(sel)[0] || null;
    };
    function parseHTML(html) {
        const root = [];
        const stack = [{ children: root }];
        const re = /<\/?([a-zA-Z0-9-]+)([^>]*)>/g;
        let match;
        while ((match = re.exec(html))) {
            const tag = match[1].toLowerCase();
            const closing = html.charAt(match.index + 1) === "/";
            if (closing) {
                if (stack.length > 1) {
                    stack.pop();
                }
                continue;
            }
            const el = new El(tag);
            const attrs = attrsFrom(match[2] || "");
            Object.keys(attrs).forEach(function (name) {
                el.setAttribute(name, attrs[name]);
            });
            const parent = stack[stack.length - 1];
            parent.children.push(el);
            el.parentNode = parent.tagName ? parent : null;
            const selfClose = tag === "input" || /\/\s*$/.test(match[2] || "");
            if (!selfClose) {
                stack.push(el);
            }
        }
        return root;
    }
    Object.defineProperty(El.prototype, "innerHTML", {
        get() {
            return this._html || "";
        },
        set(html) {
            this._html = String(html == null ? "" : html);
            this.children = parseHTML(this._html);
            for (let i = 0; i < this.children.length; i++) {
                this.children[i].parentNode = this;
            }
        }
    });

    const document = {
        activeElement: null,
        body: null,
        createElement(tag) {
            return new El(tag);
        }
    };
    document.body = document.createElement("body");
    document.activeElement = document.body;
    global.document = document;
}

test("bundle search keeps focus across input events", async () => {
    installPickerDOM();
    const root = document.createElement("div");
    const host = document.createElement("div");
    host.setAttribute("data-bundle-picker", "");
    root.appendChild(host);
    document.body.appendChild(root);
    const api = async function (url) {
        if (String(url).indexOf("bundle-components") !== -1) {
            return { data: { components: [] } };
        }
        const type = new URL(url, "http://local").searchParams.get("type");
        if (type !== "simple") {
            return { data: { products: [] } };
        }
        return {
            data: {
                products: [
                    { ID: "c1", Name: "Alpha", Slug: "alpha", Type: "simple" },
                    { ID: "c2", Name: "Beta", Slug: "beta", Type: "simple" }
                ]
            }
        };
    };
    ui.bindBundlePicker(root, { productID: "parent", api: api, canWrite: true });
    let search = null;
    for (let i = 0; i < 20 && !search; i++) {
        await new Promise(function (resolve) { setImmediate(resolve); });
        search = host.querySelector("[data-bundle-search]");
    }
    assert.ok(search, "search field should render");
    search.focus();
    search.value = "a";
    search.dispatchEvent({ type: "input", target: search });
    search.value = "al";
    search.dispatchEvent({ type: "input", target: search });
    assert.equal(host.querySelector("[data-bundle-search]"), search);
    assert.equal(document.activeElement, search);
});

async function waitFor(fn) {
    for (let i = 0; i < 30; i++) {
        const found = fn();
        if (found) {
            return found;
        }
        await new Promise(function (resolve) { setImmediate(resolve); });
    }
    return null;
}

test("bundle picker load more appends the next page and reload reuses cached leaves", async () => {
    installPickerDOM();
    const root = document.createElement("div");
    const host = document.createElement("div");
    host.setAttribute("data-bundle-picker", "");
    root.appendChild(host);
    const calls = [];
    const api = async function (url, options) {
        const method = options && options.method ? options.method : "GET";
        calls.push(method + " " + url);
        if (String(url).indexOf("bundle-components") !== -1) {
            if (method === "POST") {
                return { data: { component: { component_product_id: "s0" } } };
            }
            return {
                data: {
                    components: [
                        { component_product_id: "orphan", name: "Orphan", slug: "orphan", type: "virtual", quantity: 2, required: true }
                    ]
                }
            };
        }
        const parsed = new URL(url, "http://local");
        const type = parsed.searchParams.get("type");
        const offset = Number(parsed.searchParams.get("offset") || "0");
        if (type !== "simple") {
            return { data: { products: [] } };
        }
        if (offset === 0) {
            return { data: { products: fullPage("s") } };
        }
        return { data: { products: [{ ID: "s100", Name: "Past", Slug: "past", Type: "simple" }] } };
    };
    ui.bindBundlePicker(root, { productID: "parent", api: api, canWrite: true });
    const more = await waitFor(function () { return host.querySelector("[data-bundle-load-more]"); });
    assert.ok(more, "load more should render when the first page is full");
    assert.ok(host.querySelector('[data-bundle-toggle="orphan"]'), "assigned products missing from the page stay visible");
    const leafCallsAfterOpen = calls.filter(function (line) { return line.indexOf("/admin/products?") !== -1; }).length;
    assert.equal(leafCallsAfterOpen, 3);
    more.dispatchEvent({ type: "click" });
    const past = await waitFor(function () { return host.querySelector('[data-bundle-toggle="s100"]'); });
    assert.ok(past, "load more should append the next page");
    assert.ok(host.querySelector('[data-bundle-toggle="orphan"]'));
    const toggle = host.querySelector('[data-bundle-toggle="s0"]');
    toggle.checked = true;
    toggle.dispatchEvent({ type: "change", target: toggle });
    await waitFor(function () {
        return calls.some(function (line) { return line.indexOf("POST ") === 0; });
    });
    await waitFor(function () { return host.querySelector("[data-bundle-search]"); });
    const leafCalls = calls.filter(function (line) { return line.indexOf("/admin/products?") !== -1; });
    assert.equal(leafCalls.length, 4, "reload after a toggle must not walk leaf types again");
    assert.ok(leafCalls.some(function (line) { return line.indexOf("offset=100") !== -1; }));
});

test("grouped picker shows a failure when save throws and does not duplicate an assigned member", async () => {
    installPickerDOM();
    const root = document.createElement("div");
    const host = document.createElement("div");
    host.setAttribute("data-grouped-picker", "");
    root.appendChild(host);
    let failSave = false;
    const api = async function (url, options) {
        const method = options && options.method ? options.method : "GET";
        if (String(url).indexOf("grouped-members") !== -1 && method === "POST") {
            if (failSave) {
                throw new Error("network down");
            }
            return { error: { message: "member is not a leaf" } };
        }
        if (String(url).indexOf("grouped-members") !== -1) {
            return {
                data: {
                    members: [
                        { member_product_id: "m1", name: "Member", slug: "member", type: "simple" },
                        { member_product_id: "m1", name: "Member", slug: "member", type: "simple" }
                    ]
                }
            };
        }
        const type = new URL(url, "http://local").searchParams.get("type");
        if (type !== "simple") {
            return { data: { products: [] } };
        }
        return {
            data: {
                products: [
                    { ID: "m1", Name: "Member", Slug: "member", Type: "simple" },
                    { ID: "m2", Name: "Other", Slug: "other", Type: "simple" }
                ]
            }
        };
    };
    ui.bindGroupedPicker(root, { productID: "parent", api: api, canWrite: true });
    const toggle = await waitFor(function () { return host.querySelector('[data-grouped-toggle="m1"]'); });
    assert.ok(toggle);
    assert.equal(host.querySelectorAll('[data-grouped-toggle="m1"]').length, 1);
    const add = host.querySelector('[data-grouped-toggle="m2"]');
    add.checked = true;
    add.dispatchEvent({ type: "change", target: add });
    const alert = await waitFor(function () {
        const msg = host.querySelector("[data-grouped-msg]");
        return msg && msg.innerHTML.indexOf("member is not a leaf") !== -1 ? msg : null;
    });
    assert.ok(alert);
    failSave = true;
    const again = host.querySelector('[data-grouped-toggle="m2"]');
    again.checked = true;
    again.dispatchEvent({ type: "change", target: again });
    const saved = await waitFor(function () {
        const msg = host.querySelector("[data-grouped-msg]");
        return msg && msg.innerHTML.indexOf("Save failed") !== -1 ? msg : null;
    });
    assert.ok(saved);
});
