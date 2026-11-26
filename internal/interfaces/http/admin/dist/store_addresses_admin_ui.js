// store_addresses_admin_ui.js — Store Addresses admin helpers (PR-1204).
// Role catalog, emptiness/fallback copy, address form apply, load gate.
(function (root) {
    "use strict";

    var ADDRESS_ROLES = [
        { role: "general", label: "General", blurb: "Footer, email layout, and public identity." },
        { role: "invoice", label: "Invoice", blurb: "Invoice PDF seller block and imprint." },
        { role: "shipping_origin", label: "Shipping origin", blurb: "Warehouse / rate origin." },
        { role: "returns", label: "Returns", blurb: "RMA / return label address." }
    ];

    var FIELD_MAX = {
        line1: 255,
        line2: 255,
        city: 255,
        region: 255,
        postcode: 64,
        country: 2,
        phone: 64
    };

    function addressRoles() {
        return ADDRESS_ROLES.slice();
    }

    function roleMeta(role) {
        var code = String(role || "").toLowerCase();
        for (var i = 0; i < ADDRESS_ROLES.length; i++) {
            if (ADDRESS_ROLES[i].role === code) {
                return ADDRESS_ROLES[i];
            }
        }
        return null;
    }

    function isValidAddressRole(role) {
        return roleMeta(role) != null;
    }

    function fieldMaxLengths() {
        return {
            line1: FIELD_MAX.line1,
            line2: FIELD_MAX.line2,
            city: FIELD_MAX.city,
            region: FIELD_MAX.region,
            postcode: FIELD_MAX.postcode,
            country: FIELD_MAX.country,
            phone: FIELD_MAX.phone
        };
    }

    // Mirrors domain Address.IsEmpty (postal content; country/phone ignored).
    function addressIsEmpty(addr) {
        if (!addr) {
            return true;
        }
        function blank(v) {
            return !String(v == null ? "" : v).trim();
        }
        return blank(addr.line1) && blank(addr.line2) && blank(addr.city) &&
            blank(addr.region) && blank(addr.postcode);
    }

    function fallbackHintForRole(role, addr) {
        var code = String(role || "").toLowerCase();
        if (!addressIsEmpty(addr)) {
            return "";
        }
        var phoneNote = addr && String(addr.phone || "").trim()
            ? " Phone is stored, but postal fields are empty so fallback still applies."
            : "";
        if (code === "invoice") {
            return "Empty — invoice consumers will use the general address." + phoneNote;
        }
        if (code === "returns") {
            return "Empty — returns will use shipping origin, then general." + phoneNote;
        }
        return "";
    }

    function formatAddressSummary(addr) {
        if (addressIsEmpty(addr)) {
            var phoneOnly = addr && String(addr.phone || "").trim();
            return phoneOnly ? ("Phone only: " + phoneOnly) : "Not set";
        }
        var parts = [];
        if (addr.line1) {
            parts.push(String(addr.line1));
        }
        if (addr.line2) {
            parts.push(String(addr.line2));
        }
        if (addr.region) {
            parts.push(String(addr.region));
        }
        if (addr.city || addr.postcode) {
            parts.push([addr.postcode, addr.city].filter(Boolean).join(" ").trim());
        }
        if (addr.country) {
            parts.push(String(addr.country));
        }
        return parts.join(", ") || "Not set";
    }

    function escAttr(s) {
        return String(s == null ? "" : s)
            .replace(/&/g, "&amp;")
            .replace(/"/g, "&quot;")
            .replace(/'/g, "&#39;")
            .replace(/</g, "&lt;")
            .replace(/>/g, "&gt;");
    }

    function newStoreAddressesLoadGate() {
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

    // newAddressFillGate sequences page GETs and post-write refetches so a late
    // remount response cannot overwrite a newer fill (or the reverse).
    function newAddressFillGate() {
        var seq = 0;
        return {
            begin: function (storeID) {
                seq += 1;
                var id = seq;
                var captured = storeID || "";
                return {
                    generation: id,
                    storeID: captured,
                    isCurrent: function (currentStoreID) {
                        return id === seq && captured === (currentStoreID || "");
                    },
                    // responseStoreID may be blank on placeholders; when set it must match.
                    acceptsResponse: function (currentStoreID, responseStoreID) {
                        if (id !== seq) {
                            return false;
                        }
                        if (captured !== (currentStoreID || "")) {
                            return false;
                        }
                        if (responseStoreID != null && responseStoreID !== "" &&
                            String(responseStoreID) !== captured) {
                            return false;
                        }
                        return true;
                    }
                };
            }
        };
    }

    function storeScopeMatches(capturedStoreID, currentStoreID) {
        return (capturedStoreID || "") === (currentStoreID || "");
    }

    function shouldApplyAddressFill(fillTicket, currentStoreID, responseStoreID) {
        return !!(fillTicket && typeof fillTicket.acceptsResponse === "function" &&
            fillTicket.acceptsResponse(currentStoreID, responseStoreID));
    }

    // Message/fill only when this page generation still owns the live role form.
    function shouldMutateAddressRolePage(ticket, currentStoreID, liveFormRole, writeRole) {
        if (!ticket || typeof ticket.isCurrent !== "function") {
            return false;
        }
        if (!ticket.isCurrent(currentStoreID)) {
            return false;
        }
        return String(liveFormRole || "") === String(writeRole || "");
    }

    // Refetch into the live form when the store still matches and this role is on screen
    // (including after a same-store remount that bumped generation).
    function shouldRefetchAddressRoleAfterWrite(capturedStoreID, currentStoreID, liveFormRole, writeRole) {
        if (!storeScopeMatches(capturedStoreID, currentStoreID)) {
            return false;
        }
        return String(liveFormRole || "") === String(writeRole || "");
    }

    // Errors (and success banners) may apply on a remounted same-role page.
    function shouldShowAddressWriteFeedback(capturedStoreID, currentStoreID, liveFormRole, writeRole) {
        return shouldRefetchAddressRoleAfterWrite(capturedStoreID, currentStoreID, liveFormRole, writeRole);
    }

    function shouldRefreshAddressIndexAfterWrite(capturedStoreID, currentStoreID, gridConnected) {
        return !!gridConnected && storeScopeMatches(capturedStoreID, currentStoreID);
    }

    // addressRefetchPaintAction decides how a post-write GET paints.
    // "build" is for the remount loading shell (data-role set, no inputs yet).
    function addressRefetchPaintAction(fillAccepted, hasLiveForm, formReady, hasError) {
        if (!fillAccepted || !hasLiveForm) {
            return "skip";
        }
        if (hasError) {
            return formReady ? "error-ready" : "error-shell";
        }
        return formReady ? "fill" : "build";
    }

    function firstValidationMessage(form) {
        if (!form) {
            return "Fix the highlighted fields before saving.";
        }
        var invalid = form.querySelector(":invalid");
        if (invalid && invalid.validationMessage) {
            return String(invalid.validationMessage);
        }
        return "Fix the highlighted fields before saving.";
    }

    function applyAddressValues(targets, addr) {
        addr = addr || {};
        if (!targets) {
            return;
        }
        if (targets.line1) {
            targets.line1.value = addr.line1 || "";
        }
        if (targets.line2) {
            targets.line2.value = addr.line2 || "";
        }
        if (targets.city) {
            targets.city.value = addr.city || "";
        }
        if (targets.region) {
            targets.region.value = addr.region || "";
        }
        if (targets.postcode) {
            targets.postcode.value = addr.postcode || "";
        }
        if (targets.country) {
            targets.country.value = addr.country || "";
        }
        if (targets.phone) {
            targets.phone.value = addr.phone || "";
        }
    }

    // Always include every key, including "" clears — AddressPatch treats omitted as unchanged.
    function buildAddressPutBody(fields) {
        fields = fields || {};
        return {
            line1: fields.line1 == null ? "" : String(fields.line1),
            line2: fields.line2 == null ? "" : String(fields.line2),
            city: fields.city == null ? "" : String(fields.city),
            region: fields.region == null ? "" : String(fields.region),
            postcode: fields.postcode == null ? "" : String(fields.postcode),
            country: fields.country == null ? "" : String(fields.country),
            phone: fields.phone == null ? "" : String(fields.phone)
        };
    }

    function scopeBannerCopy(store, storeID) {
        var id = storeID || (store && store.id) || "";
        if (!store && !id) {
            return {
                kind: "empty",
                text: "No store selected. Store addresses have no global record. Select a store in the header switcher."
            };
        }
        if (!store && id) {
            return {
                kind: "unknown",
                text: "Editing store id " + id + " (not in the store list). Addresses still save to this store scope; there is no global fallback."
            };
        }
        var name = store.name || store.code || store.id || "store";
        var country = store.country || "—";
        return {
            kind: "store",
            text: "Editing store: " + name + " (" + country + "). Addresses are store-scoped; there is no global fallback."
        };
    }

    function indexByRole(addresses) {
        var map = {};
        for (var i = 0; i < (addresses || []).length; i++) {
            var a = addresses[i];
            if (a && a.role) {
                map[a.role] = a;
            }
        }
        return map;
    }

    var api = {
        addressRoles: addressRoles,
        roleMeta: roleMeta,
        isValidAddressRole: isValidAddressRole,
        fieldMaxLengths: fieldMaxLengths,
        addressIsEmpty: addressIsEmpty,
        fallbackHintForRole: fallbackHintForRole,
        formatAddressSummary: formatAddressSummary,
        escAttr: escAttr,
        newStoreAddressesLoadGate: newStoreAddressesLoadGate,
        newAddressFillGate: newAddressFillGate,
        storeScopeMatches: storeScopeMatches,
        shouldApplyAddressFill: shouldApplyAddressFill,
        shouldMutateAddressRolePage: shouldMutateAddressRolePage,
        shouldRefetchAddressRoleAfterWrite: shouldRefetchAddressRoleAfterWrite,
        shouldShowAddressWriteFeedback: shouldShowAddressWriteFeedback,
        shouldRefreshAddressIndexAfterWrite: shouldRefreshAddressIndexAfterWrite,
        addressRefetchPaintAction: addressRefetchPaintAction,
        firstValidationMessage: firstValidationMessage,
        applyAddressValues: applyAddressValues,
        buildAddressPutBody: buildAddressPutBody,
        scopeBannerCopy: scopeBannerCopy,
        indexByRole: indexByRole
    };

    root.ShopandaStoreAddressesAdminUI = api;
    if (typeof module !== "undefined" && module.exports) {
        module.exports = api;
    }
})(typeof globalThis !== "undefined" ? globalThis : this);
