// store_information_admin_ui.js — Store Information admin helpers (PR-1203 CR).
// Load gates + attribute escaping + tax upsert helpers. No DOM required for tests.
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

    // newStoreInfoLoadGate drops stale page-load callbacks after a newer load
    // starts or when the header store id no longer matches the capture at start.
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

    // newTaxPanelLoadGate sequences tax panel refreshes. Call start() before
    // GET /admin/tax-identifiers (not after it resolves). A slow earlier list
    // response must keep its old ticket so it cannot paint over a newer refresh.
    function newTaxPanelLoadGate() {
        var seq = 0;
        return {
            start: function () {
                seq += 1;
                var id = seq;
                return {
                    generation: id,
                    isCurrent: function () {
                        return id === seq;
                    }
                };
            }
        };
    }

    // shouldPaintTaxPanel mirrors the list-GET callback guard: drop when a
    // newer refresh already called start().
    function shouldPaintTaxPanel(panelTicket) {
        return !!(panelTicket && typeof panelTicket.isCurrent === "function" && panelTicket.isCurrent());
    }

    function storeScopeMatches(capturedStoreID, currentStoreID) {
        return (capturedStoreID || "") === (currentStoreID || "");
    }

    // Write responses must apply when the store id still matches, even if a
    // same-store remount bumped the page generation (e.g. language change).
    function shouldApplyWriteResult(capturedStoreID, currentStoreID) {
        return storeScopeMatches(capturedStoreID, currentStoreID);
    }

    // Language/currency do not affect Store Information payloads; remounting
    // mid-save drops the write callback and can refetch pre-write rows.
    function headerContextChangeRequiresRemount(path) {
        if (path === "/admin/settings/store-information") {
            return false;
        }
        // Store Addresses is a sibling settings surface (PR-1204).
        if (path === "/admin/settings/store-addresses" ||
            (path && path.indexOf("/admin/settings/store-addresses/") === 0)) {
            return false;
        }
        return true;
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

    function applyGeneralStoreValues(targets, store) {
        store = store || {};
        if (!targets) {
            return;
        }
        if (targets.code) {
            targets.code.value = store.code || "";
        }
        if (targets.name) {
            targets.name.value = store.name || "";
        }
        if (targets.domain) {
            targets.domain.value = store.domain || "";
        }
        if (targets.country) {
            targets.country.value = store.country || "";
        }
        if (targets.language) {
            targets.language.value = store.language || "";
        }
        if (targets.currency) {
            targets.currency.value = store.currency || "";
        }
        if (targets.is_default) {
            targets.is_default.checked = !!store.is_default;
        }
    }

    function composeSuccessReloadMessage(successMessage, errorMessage) {
        var success = successMessage ? String(successMessage) : "";
        var err = errorMessage ? String(errorMessage) : "unknown error";
        if (success) {
            return success + " Reload failed: " + err;
        }
        return "Reload failed: " + err;
    }

    // setTaxPanelActionsLocked disables save + delete (+ form fields) for the
    // whole in-flight write so a DELETE cannot race a PUT and recreate a row.
    function setTaxPanelActionsLocked(controls, locked) {
        for (var i = 0; i < (controls || []).length; i++) {
            if (controls[i]) {
                controls[i].disabled = !!locked;
            }
        }
    }

    // store may be null when the header id is missing from /admin/stores.
    // Pass storeID so an unknown id is not shown as "no store selected".
    function scopeBannerCopy(store, storeID) {
        var id = storeID || (store && store.id) || "";
        if (!store && !id) {
            return {
                kind: "empty",
                text: "No store selected. Store information has no global record. Select a store in the header switcher."
            };
        }
        if (!store && id) {
            return {
                kind: "unknown",
                text: "Editing store id " + id + " (not in the store list). Identity and tax IDs still save to this store scope; there is no global fallback."
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
        newTaxPanelLoadGate: newTaxPanelLoadGate,
        shouldPaintTaxPanel: shouldPaintTaxPanel,
        storeScopeMatches: storeScopeMatches,
        shouldApplyWriteResult: shouldApplyWriteResult,
        headerContextChangeRequiresRemount: headerContextChangeRequiresRemount,
        sidebarLinkIsActive: sidebarLinkIsActive,
        existingTaxValue: existingTaxValue,
        taxSubmitLabel: taxSubmitLabel,
        shouldConfirmTaxOverwrite: shouldConfirmTaxOverwrite,
        partitionTaxTypes: partitionTaxTypes,
        buildGeneralStorePutBody: buildGeneralStorePutBody,
        applyIdentityValues: applyIdentityValues,
        applyGeneralStoreValues: applyGeneralStoreValues,
        composeSuccessReloadMessage: composeSuccessReloadMessage,
        setTaxPanelActionsLocked: setTaxPanelActionsLocked,
        scopeBannerCopy: scopeBannerCopy
    };

    root.ShopandaStoreInformationAdminUI = api;
    if (typeof module !== "undefined" && module.exports) {
        module.exports = api;
    }
})(typeof globalThis !== "undefined" ? globalThis : this);
