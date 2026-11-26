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
        if (!addressIsEmpty(addr)) {
            return "";
        }
        if (role === "invoice") {
            return "Empty — invoice consumers will use the general address.";
        }
        if (role === "returns") {
            return "Empty — returns will use shipping origin, then general.";
        }
        return "";
    }

    function formatAddressSummary(addr) {
        if (addressIsEmpty(addr)) {
            return "Not set";
        }
        var parts = [];
        if (addr.line1) {
            parts.push(String(addr.line1));
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

    function storeScopeMatches(capturedStoreID, currentStoreID) {
        return (capturedStoreID || "") === (currentStoreID || "");
    }

    function shouldApplyWriteResult(capturedStoreID, currentStoreID) {
        return storeScopeMatches(capturedStoreID, currentStoreID);
    }

    function headerContextChangeRequiresRemount(path) {
        if (path === "/admin/settings/store-addresses") {
            return false;
        }
        if (path && path.indexOf("/admin/settings/store-addresses/") === 0) {
            return false;
        }
        return true;
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

    function buildAddressPutBody(fields) {
        fields = fields || {};
        return {
            line1: fields.line1 || "",
            line2: fields.line2 || "",
            city: fields.city || "",
            region: fields.region || "",
            postcode: fields.postcode || "",
            country: fields.country || "",
            phone: fields.phone || ""
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
        addressIsEmpty: addressIsEmpty,
        fallbackHintForRole: fallbackHintForRole,
        formatAddressSummary: formatAddressSummary,
        escAttr: escAttr,
        newStoreAddressesLoadGate: newStoreAddressesLoadGate,
        storeScopeMatches: storeScopeMatches,
        shouldApplyWriteResult: shouldApplyWriteResult,
        headerContextChangeRequiresRemount: headerContextChangeRequiresRemount,
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
