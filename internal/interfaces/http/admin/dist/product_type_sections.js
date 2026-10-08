// product_type_sections.js — Product type section visibility helpers (PR-1053).
// Loaded before admin.js in the SPA; also CommonJS-exported for Node tests.
// Type display labels come from product.form field options (Go ProductTypeLabel),
// not a duplicated map here — grid/form share one server-driven source.
(function (root) {
    function sectionTypes(attr) {
        return String(attr || "").split(",").map(function (s) {
            return String(s || "").trim();
        }).filter(function (s) {
            return s.length > 0;
        });
    }

    // Empty types list (empty attribute) → always visible.
    function sectionVisible(attr, selectedType) {
        var types = sectionTypes(attr);
        var selected = String(selectedType || "simple");
        return types.length === 0 || types.indexOf(selected) !== -1;
    }

    function syncSections(root, selectedType) {
        if (!root || typeof root.querySelectorAll !== "function") {
            return;
        }
        var nodes = root.querySelectorAll("[data-product-type-section]");
        for (var i = 0; i < nodes.length; i++) {
            var show = sectionVisible(nodes[i].getAttribute("data-product-type-section"), selectedType);
            // Only toggle display; leave other inline styles alone.
            nodes[i].style.display = show ? "" : "none";
        }
    }

    function optionValues(options) {
        var out = [];
        if (!options) {
            return out;
        }
        for (var i = 0; i < options.length; i++) {
            out.push(String(options[i].value));
        }
        return out;
    }

    // When stored type is not in schema options, keep a selected option with the
    // raw value so save cannot silently coerce to the first listed type.
    function selectNeedsUnknownOption(options, selectedValue) {
        var selected = String(selectedValue || "");
        if (!selected) {
            return false;
        }
        var known = optionValues(options);
        return known.indexOf(selected) === -1;
    }

    var api = {
        sectionTypes: sectionTypes,
        sectionVisible: sectionVisible,
        syncSections: syncSections,
        selectNeedsUnknownOption: selectNeedsUnknownOption
    };

    root.ShopandaProductTypeSections = api;
    if (typeof module !== "undefined" && module.exports) {
        module.exports = api;
    }
})(typeof globalThis !== "undefined" ? globalThis : this);
