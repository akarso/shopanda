// product_type_sections.js — Product type admin helpers (PR-1053).
// Loaded before admin.js in the SPA; also CommonJS-exported for Node tests.
// Type display labels come from product.form field options (Go ProductTypeLabel).
(function (root) {
    var TYPE_CHANGE_CONFIRM =
        "Changing type can alter shipping (virtual/downloadable skip physical shipping) and which type-specific panels apply. Continue?";

    var CORE_PRODUCT_FIELDS = {
        name: true,
        slug: true,
        description: true,
        status: true,
        type: true
    };

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
        return optionValues(options).indexOf(selected) === -1;
    }

    function labelsFromForm(form) {
        var map = {};
        if (!form || !Array.isArray(form.fields)) {
            return map;
        }
        for (var i = 0; i < form.fields.length; i++) {
            var field = form.fields[i];
            if (!field || field.name !== "type") {
                continue;
            }
            var options = field.options || [];
            for (var j = 0; j < options.length; j++) {
                var o = options[j] || {};
                var key = String(o.value == null ? "" : o.value);
                if (key) {
                    map[key] = o.label != null && o.label !== "" ? String(o.label) : key;
                }
            }
        }
        return map;
    }

    function displayLabel(value, labels) {
        var key = value == null ? "" : String(value);
        if (labels && Object.prototype.hasOwnProperty.call(labels, key)) {
            return labels[key];
        }
        return key;
    }

    function applyTypeCellLabels(root, labels) {
        if (!root || typeof root.querySelectorAll !== "function") {
            return;
        }
        var cells = root.querySelectorAll("[data-product-type-cell]");
        for (var i = 0; i < cells.length; i++) {
            var raw = cells[i].getAttribute("data-product-type-cell");
            cells[i].textContent = displayLabel(raw, labels);
        }
    }

    // confirmFn(message) → boolean. Cancel restores previousType and leaves baseline unchanged.
    function resolveTypeChange(opts) {
        opts = opts || {};
        var previous = String(opts.previousType || "simple");
        var next = String(opts.nextType || "simple");
        if (opts.confirmOnChange && next !== previous) {
            var ok = typeof opts.confirmFn === "function" ? opts.confirmFn(TYPE_CHANGE_CONFIRM) : true;
            if (!ok) {
                return { type: previous, previousType: previous, cancelled: true };
            }
            return { type: next, previousType: next, cancelled: false };
        }
        return { type: next, previousType: previous, cancelled: false };
    }

    function isCoreProductField(name) {
        return !!CORE_PRODUCT_FIELDS[name];
    }

    // Mirrors collectProductPayload field routing for the type selector path.
    function assignProductField(payload, name, value) {
        if (!payload.attributes) {
            payload.attributes = {};
        }
        if (isCoreProductField(name)) {
            payload[name] = value;
        } else {
            payload.attributes[name] = value;
        }
        return payload;
    }

    var api = {
        TYPE_CHANGE_CONFIRM: TYPE_CHANGE_CONFIRM,
        sectionTypes: sectionTypes,
        sectionVisible: sectionVisible,
        syncSections: syncSections,
        selectNeedsUnknownOption: selectNeedsUnknownOption,
        labelsFromForm: labelsFromForm,
        displayLabel: displayLabel,
        applyTypeCellLabels: applyTypeCellLabels,
        resolveTypeChange: resolveTypeChange,
        isCoreProductField: isCoreProductField,
        assignProductField: assignProductField
    };

    root.ShopandaProductTypeSections = api;
    if (typeof module !== "undefined" && module.exports) {
        module.exports = api;
    }
})(typeof globalThis !== "undefined" ? globalThis : this);
