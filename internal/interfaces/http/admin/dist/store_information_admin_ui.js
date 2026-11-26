// store_information_admin_ui.js — Store Information admin helpers (PR-1203 CR).
// Load gate + attribute escaping + tax upsert helpers. No DOM required for tests.
(function (root) {
    "use strict";

    // escAttr encodes for HTML attribute values (quotes included).
    // Prefer setting input.value via DOM properties when a live form exists.
    function escAttr(s) {
        return String(s == null ? "" : s)
            .replace(/&/g, "&amp;")
            .replace(/"/g, "&quot;")
            .replace(/</g, "&lt;")
            .replace(/>/g, "&gt;");
    }

    // newStoreInfoLoadGate drops stale callbacks after a newer load starts
    // or when the header store id no longer matches the capture at start.
    function newStoreInfoLoadGate() {
        var seq = 0;
        return {
            start: function (storeID) {
                seq += 1;
                var id = seq;
                var captured = storeID || "";
                return {
                    generation: id,
                    storeID: captured,
                    isCurrent: function (currentStoreID) {
                        return id === seq && captured === (currentStoreID || "");
                    }
                };
            }
        };
    }

    function sidebarLinkIsActive(href, currentPath) {
        if (!href || !currentPath) {
            return false;
        }
        // Exact match for General settings so Store Information is not current too.
        if (href === "/admin/settings") {
            return currentPath === "/admin/settings";
        }
        if (href === "/admin") {
            return false;
        }
        return currentPath.indexOf(href) === 0 &&
            (currentPath.length === href.length || currentPath.charAt(href.length) === "/");
    }

    function existingTaxValue(taxIDs, type) {
        if (!type || !taxIDs) {
            return null;
        }
        for (var i = 0; i < taxIDs.length; i++) {
            if (taxIDs[i] && taxIDs[i].type === type) {
                return taxIDs[i].value;
            }
        }
        return null;
    }

    function taxSubmitLabel(existing) {
        return existing != null ? "Replace tax ID" : "Save tax ID";
    }

    function shouldConfirmTaxOverwrite(existing, nextValue) {
        return existing != null && String(existing) !== String(nextValue == null ? "" : nextValue);
    }

    // partitionTaxTypes splits all types into applicable vs other by code set.
    // When countryApplicable is null/undefined (filter failed), treat all as applicable.
    function partitionTaxTypes(allTypes, countryApplicable, countryFilterFailed) {
        var all = allTypes || [];
        if (countryFilterFailed) {
            return { applicable: all.slice(), other: [] };
        }
        var applicable = countryApplicable || [];
        var codes = {};
        for (var i = 0; i < applicable.length; i++) {
            if (applicable[i] && applicable[i].code) {
                codes[applicable[i].code] = true;
            }
        }
        var other = [];
        for (var j = 0; j < all.length; j++) {
            if (all[j] && all[j].code && !codes[all[j].code]) {
                other.push(all[j]);
            }
        }
        return { applicable: applicable, other: other };
    }

    function buildGeneralStorePutBody(fields) {
        fields = fields || {};
        return {
            code: fields.code || "",
            name: fields.name || "",
            domain: fields.domain || "",
            country: fields.country || "",
            language: fields.language || "",
            currency: fields.currency || "",
            is_default: !!fields.is_default
        };
    }

    function applyIdentityValues(targets, identity) {
        identity = identity || {};
        if (!targets) {
            return;
        }
        if (targets.legal_name) {
            targets.legal_name.value = identity.legal_name || "";
        }
        if (targets.phone) {
            targets.phone.value = identity.phone || "";
        }
        if (targets.hours) {
            targets.hours.value = identity.hours || "";
        }
        if (targets.email_general) {
            targets.email_general.value = identity.email_general || "";
        }
        if (targets.email_sales) {
            targets.email_sales.value = identity.email_sales || "";
        }
        if (targets.email_support) {
            targets.email_support.value = identity.email_support || "";
        }
    }

    function scopeBannerCopy(store) {
        if (!store) {
            return {
                kind: "empty",
                text: "No store selected. Store information has no global record. Select a store in the header switcher."
            };
        }
        var name = store.name || store.code || store.id || "store";
        var country = store.country || "—";
        return {
            kind: "store",
            text: "Editing store: " + name + " (" + country + "). Identity and tax IDs are store-scoped; there is no global fallback."
        };
    }

    var api = {
        escAttr: escAttr,
        newStoreInfoLoadGate: newStoreInfoLoadGate,
        sidebarLinkIsActive: sidebarLinkIsActive,
        existingTaxValue: existingTaxValue,
        taxSubmitLabel: taxSubmitLabel,
        shouldConfirmTaxOverwrite: shouldConfirmTaxOverwrite,
        partitionTaxTypes: partitionTaxTypes,
        buildGeneralStorePutBody: buildGeneralStorePutBody,
        applyIdentityValues: applyIdentityValues,
        scopeBannerCopy: scopeBannerCopy
    };

    root.ShopandaStoreInformationAdminUI = api;
    if (typeof module !== "undefined" && module.exports) {
        module.exports = api;
    }
})(typeof globalThis !== "undefined" ? globalThis : this);
