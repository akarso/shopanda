// product_visibility_section.js — Product visibility admin GUI (PR-1059).
// Loaded before admin.js in the SPA; also CommonJS-exported for Node tests.
// Renders the four VisibilityAxes mode selects (auto/visible/hidden) into the
// type-agnostic "visibility" product.form section, with an override badge and
// a "why hidden" explanation (failing AutoBasis conditions) when an axis is
// auto and AutoBasis is currently false.
//
// The GET /admin/products/{id} "visibility" payload reports both
// a domain-level "resolved" boolean (ResolveAxis — ignores product Status)
// and a public-gate "effective" boolean (EffectiveAxis — the real storefront
// outcome; a forced-visible axis on a non-active product is never effective).
// The badge always describes the storefront-facing "effective" truth, and is
// recomputed client-side from {mode, autoBasis, status} on every select
// change via effectiveForMode — never from a stale per-axis "effective" that
// was only valid for whichever mode happened to be persisted at load time.
//
// State has three shapes (see stateFromVisibilityPayload):
//   - "live"  — a visibility payload loaded; badges reflect real facts.
//   - "error" — an existing product whose facts load failed; one explicit
//     "unavailable" message replaces every badge (never the misleading
//     "computed after save" pending text).
//   - null    — pending: either a true pre-save create (no payload fetched
//     at all) or an existing product with no snapshot yet and no error
//     (rare — see product_admin.go; a concurrent delete between FindByID and
//     the facts query, since the facts query itself has no "first refresh"
//     delay). Both render the same "computed after save" text.
//
// Section-level captions (not per-badge prefixes):
//   - "Enforcement off — preview" when the live payload's enforced is false,
//     instead of prefixing every individual badge with "Would be ...".
//   - "Preview as of form status (save to persist modes)." whenever live
//     state is rendered, since bindStatusChange recomputes badges from the
//     *selected* (possibly unsaved) status, not the persisted one.
(function (root) {
    // Axis metadata. "pascal" matches the Go VisibilityAxes field names as
    // serialized (no JSON tags -> PascalCase) for reading the product's
    // persisted modes; "key" is the lowercase name used in the outbound
    // visibility_modes request payload (matches visibilityModesRequest in
    // product_admin.go), in the GET response's "axes" object, and in DOM ids.
    var AXES = [
        { key: "catalog", pascal: "Catalog", label: "Catalog" },
        { key: "search", pascal: "Search", label: "Search" },
        { key: "individually", pascal: "Individually", label: "Individually" },
        { key: "purchasable", pascal: "Purchasable", label: "Purchasable" }
    ];

    var MODES = ["auto", "visible", "hidden"];

    // Stable AutoBasis failure codes (catalog.AutoBasisCode* in Go) mapped to
    // merchant-facing labels. Keep in sync with internal/domain/catalog/visibility.go.
    var FAILURE_LABELS = {
        status_inactive: "status is not active",
        no_stock: "no variant in stock",
        no_price: "no price set",
        no_store_assignment: "no store assignment",
        no_category: "no category assignment"
    };

    function failureLabel(code) {
        return FAILURE_LABELS[code] || String(code);
    }

    var ERROR_BADGE_TEXT = "Visibility facts unavailable — reload to retry";
    var ENFORCEMENT_OFF_CAPTION = "Enforcement off — preview";
    var STATUS_FRESHNESS_CAPTION = "Preview as of form status (save to persist modes).";

    // product.visibilityModes is set by admin.js normalizeProduct via
    // pick(raw, "visibility_modes", "VisibilityModes"); the raw value from the
    // GET /admin/products/{id} response is VisibilityAxes (PascalCase: Catalog/
    // Search/Individually/Purchasable), each a VisibilityMode string.
    function modesFromProduct(product) {
        var raw = (product && product.visibilityModes) || {};
        var out = {};
        for (var i = 0; i < AXES.length; i++) {
            var axis = AXES[i];
            var v = raw[axis.pascal] != null ? raw[axis.pascal] : raw[axis.key];
            out[axis.key] = (v && String(v)) || "auto";
        }
        return out;
    }

    function defaultModes() {
        var out = {};
        for (var i = 0; i < AXES.length; i++) {
            out[AXES[i].key] = "auto";
        }
        return out;
    }

    // deriveAutoBasis / deriveFailureCodes recompute auto_basis/failure_codes
    // client-side from {status, inputFailureCodes} alone, without re-fetching
    // facts. inputFailureCodes matches catalog.AutoBasisInputFailureCodes
    // (every AutoBasisFailureCodes check except status_inactive); status is
    // the only AutoBasis input that tracks the form, not persisted facts.
    // Keep in sync with catalog.AutoBasis and catalog.AutoBasisInputFailureCodes
    // in internal/domain/catalog/visibility.go.
    function deriveAutoBasis(status, inputFailureCodes) {
        return status === "active" && (!inputFailureCodes || inputFailureCodes.length === 0);
    }

    function deriveFailureCodes(status, inputFailureCodes) {
        var codes = (inputFailureCodes || []).slice();
        if (status !== "active") {
            codes = ["status_inactive"].concat(codes);
        }
        return codes;
    }

    function axisExplanationFromPayload(raw) {
        raw = raw || {};
        return {
            mode: raw.mode || "auto",
            override: !!raw.override,
            resolved: !!raw.resolved,
            effective: !!raw.effective,
            failureCodes: Array.isArray(raw.failure_codes) ? raw.failure_codes : []
        };
    }

    // visibilityPayload is the GET /admin/products/{id} "visibility" object:
    // { enforced, status, auto_basis, failure_codes, axes: {catalog:{mode,
    // override,resolved,effective,failure_codes}, ...} }. errorText is the
    // sibling "visibility_error" string (set instead of a payload when an
    // existing product's facts failed to load).
    //
    // Returns one of three shapes — see the file header comment:
    //   - payload truthy       -> { kind: "live", ... }
    //   - !payload && errorText -> { kind: "error", error: errorText }
    //   - !payload && !errorText -> null (pending: create, or the rare
    //     concurrent-delete missing-snapshot case)
    function stateFromVisibilityPayload(payload, errorText) {
        if (payload) {
            var rawAxes = payload.axes || {};
            var axes = {};
            for (var i = 0; i < AXES.length; i++) {
                var key = AXES[i].key;
                axes[key] = axisExplanationFromPayload(rawAxes[key]);
            }
            var status = String(payload.status || "");
            // autoBasis/failureCodes are derived from {status, inputFailureCodes}
            // via deriveAutoBasis/deriveFailureCodes (same path as bindStatusChange),
            // not from payload.auto_basis/failure_codes (saved status only).
            var inputFailureCodes = Array.isArray(payload.input_failure_codes) ? payload.input_failure_codes : [];
            return {
                kind: "live",
                enforced: !!payload.enforced,
                status: status,
                inputFailureCodes: inputFailureCodes,
                autoBasis: deriveAutoBasis(status, inputFailureCodes),
                failureCodes: deriveFailureCodes(status, inputFailureCodes),
                axes: axes
            };
        }
        if (errorText) {
            return { kind: "error", error: String(errorText) };
        }
        return null;
    }

    function isLive(state) {
        return !!state && state.kind === "live";
    }

    function isError(state) {
        return !!state && state.kind === "error";
    }

    function selectID(axisKey) {
        return "product-visibility-mode-" + axisKey;
    }

    function badgeID(axisKey) {
        return "product-visibility-badge-" + axisKey;
    }

    function failuresID(axisKey) {
        return "product-visibility-failures-" + axisKey;
    }

    function capitalize(s) {
        s = String(s == null ? "" : s);
        return s.length ? s.charAt(0).toUpperCase() + s.slice(1) : s;
    }

    // effectiveForMode mirrors catalog.EffectiveAxis: a non-active product is
    // never allowed on any surface, even when the mode is forced visible.
    // Computed client-side on every mode change (and on status change — see
    // bindStatusChange) so the badge reflects the candidate mode/status pair,
    // not whatever happened to be persisted when the page loaded.
    function effectiveForMode(mode, autoBasis, status) {
        if (status !== "active") {
            return false;
        }
        if (mode === "auto") {
            return !!autoBasis;
        }
        if (mode === "visible") {
            return true;
        }
        if (mode === "hidden") {
            return false;
        }
        return false;
    }

    // badgeText describes the storefront-facing outcome for mode given state.
    // No per-badge "Would be ..." prefix; see renderSectionCaptionsHTML for
    // the section-level "Enforcement off — preview" caption.
    function badgeText(mode, state) {
        if (!state) {
            return capitalize(mode) + " — computed after save";
        }
        if (isError(state)) {
            return ERROR_BADGE_TEXT;
        }
        var effective = effectiveForMode(mode, state.autoBasis, state.status);
        if (mode === "auto") {
            return "Auto — " + (effective ? "visible" : "hidden");
        }
        if (mode === "hidden") {
            return "Forced hidden";
        }
        if (mode === "visible") {
            return effective ? "Forced visible" : "Forced visible — not live (product inactive)";
        }
        return capitalize(mode);
    }

    // "why hidden" only applies to a live axis that is auto and currently
    // failing AutoBasis; forced axes, a pending/create state, and an error
    // state have nothing to explain.
    function showFailures(mode, state) {
        return isLive(state) && mode === "auto" && !state.autoBasis;
    }

    function failureCodesFor(state) {
        return (isLive(state) && state.failureCodes) || [];
    }

    function escapeHTML(str) {
        return String(str == null ? "" : str)
            .replace(/&/g, "&amp;")
            .replace(/</g, "&lt;")
            .replace(/>/g, "&gt;")
            .replace(/"/g, "&quot;")
            .replace(/'/g, "&#39;");
    }

    function renderOptionsHTML(selectedMode) {
        var selected = String(selectedMode || "auto");
        var html = "";
        for (var i = 0; i < MODES.length; i++) {
            var m = MODES[i];
            html += '<option value="' + m + '"' + (m === selected ? " selected" : "") + '>' + m + "</option>";
        }
        return html;
    }

    // renderFailureItemsHTML builds just the <li> items (no wrapping <ul>),
    // shared by renderFailuresHTML and updateRowFromMode.
    function renderFailureItemsHTML(codes) {
        var items = "";
        for (var i = 0; i < (codes || []).length; i++) {
            items += "<li>" + escapeHTML(failureLabel(codes[i])) + "</li>";
        }
        return items;
    }

    function renderFailuresHTML(axisKey, codes, visible) {
        return '<ul id="' + failuresID(axisKey) + '" class="product-visibility-failures"' +
            (visible ? "" : ' style="display:none;"') +
            ">" + renderFailureItemsHTML(codes) + "</ul>";
    }

    // state: result of stateFromVisibilityPayload (live / error / null).
    function renderRowHTML(axis, mode, state) {
        var visible = showFailures(mode, state);
        return '<div class="product-visibility-row" data-visibility-axis="' + axis.key + '">' +
            '<label for="' + selectID(axis.key) + '">' + escapeHTML(axis.label) + "</label>" +
            '<select id="' + selectID(axis.key) + '" name="visibility_mode_' + axis.key + '" data-visibility-mode="' + axis.key + '">' +
            renderOptionsHTML(mode) +
            "</select>" +
            '<span id="' + badgeID(axis.key) + '" class="product-visibility-badge">' + escapeHTML(badgeText(mode, state)) + "</span>" +
            renderFailuresHTML(axis.key, failureCodesFor(state), visible) +
            "</div>";
    }

    // One section-level caption per concern — never per-badge:
    //   - enforcement-off preview note (only for live + !enforced)
    //   - status-freshness note (only for live — the status select rebinds
    //     and re-renders badges; see bindStatusChange)
    function renderSectionCaptionsHTML(state) {
        if (!isLive(state)) {
            return "";
        }
        var html = "";
        if (!state.enforced) {
            html += '<p id="product-visibility-enforcement-caption" class="product-visibility-caption product-visibility-caption-enforcement">' +
                escapeHTML(ENFORCEMENT_OFF_CAPTION) + "</p>";
        }
        html += '<p id="product-visibility-status-caption" class="product-visibility-caption product-visibility-caption-status">' +
            escapeHTML(STATUS_FRESHNESS_CAPTION) + "</p>";
        return html;
    }

    // modes: {catalog,search,individually,purchasable} -> mode string.
    // state: result of stateFromVisibilityPayload (live / error / null).
    function renderSectionHTML(modes, state) {
        modes = modes || defaultModes();
        var html = renderSectionCaptionsHTML(state);
        for (var i = 0; i < AXES.length; i++) {
            var axis = AXES[i];
            html += renderRowHTML(axis, modes[axis.key] || "auto", state);
        }
        return html;
    }

    function collectModes(root) {
        var doc = root || (typeof document !== "undefined" ? document : null);
        var out = {};
        for (var i = 0; i < AXES.length; i++) {
            var axis = AXES[i];
            var el = doc && doc.getElementById ? doc.getElementById(selectID(axis.key)) : null;
            out[axis.key] = el && el.value ? el.value : "auto";
        }
        return out;
    }

    // Re-renders badge and failure list (visibility and content) for one axis.
    // autoBasis/failureCodes on state are refreshed by bindStatusChange before
    // this runs when status changes.
    function updateRowFromMode(doc, axis, mode, state) {
        var badge = doc.getElementById(badgeID(axis.key));
        if (badge) {
            badge.textContent = badgeText(mode, state);
        }
        var list = doc.getElementById(failuresID(axis.key));
        if (list) {
            list.style.display = showFailures(mode, state) ? "" : "none";
            list.innerHTML = renderFailureItemsHTML(failureCodesFor(state));
        }
    }

    function bindAxisChange(root, state) {
        var doc = root || (typeof document !== "undefined" ? document : null);
        if (!doc || typeof doc.getElementById !== "function") {
            return;
        }
        for (var i = 0; i < AXES.length; i++) {
            var axis = AXES[i];
            (function (axis) {
                var el = doc.getElementById(selectID(axis.key));
                if (!el || el.getAttribute("data-visibility-change-bound")) {
                    return;
                }
                el.setAttribute("data-visibility-change-bound", "1");
                el.addEventListener("change", function () {
                    updateRowFromMode(doc, axis, el.value, state);
                });
            })(axis);
        }
    }

    // refreshLivePreview recomputes autoBasis/failureCodes from fresh
    // inputFailureCodes and the form's current status (when present), then
    // re-renders every axis badge/failure list without changing mode selects.
    // Only meaningful for live state — pending/create and error states no-op.
    function refreshLivePreview(doc, form, state, inputFailureCodes) {
        if (!doc || !isLive(state)) {
            return;
        }
        var status = state.status;
        if (form && form.elements && form.elements.status) {
            status = String(form.elements.status.value || "");
        }
        state.inputFailureCodes = (inputFailureCodes || []).slice();
        state.status = status;
        state.autoBasis = deriveAutoBasis(state.status, state.inputFailureCodes);
        state.failureCodes = deriveFailureCodes(state.status, state.inputFailureCodes);
        for (var i = 0; i < AXES.length; i++) {
            var axis = AXES[i];
            var modeEl = doc.getElementById(selectID(axis.key));
            var mode = modeEl && modeEl.value ? modeEl.value : "auto";
            updateRowFromMode(doc, axis, mode, state);
        }
    }

    // bindStatusChange wires a change listener on the main product form's
    // "status" select so every axis badge previews the *selected* (possibly
    // unsaved) status, not the one the page loaded with. Mutates state in
    // place so a later axis-mode change (via bindAxisChange/updateRowFromMode)
    // also sees the latest selection.
    function bindStatusChange(doc, form, state) {
        if (!doc || !form || !form.elements || !isLive(state)) {
            return;
        }
        var statusEl = form.elements.status;
        if (!statusEl || statusEl.getAttribute("data-visibility-status-change-bound")) {
            return;
        }
        statusEl.setAttribute("data-visibility-status-change-bound", "1");
        statusEl.addEventListener("change", function () {
            refreshLivePreview(doc, form, state, state.inputFailureCodes);
        });
    }

    var api = {
        AXES: AXES,
        MODES: MODES,
        FAILURE_LABELS: FAILURE_LABELS,
        ERROR_BADGE_TEXT: ERROR_BADGE_TEXT,
        ENFORCEMENT_OFF_CAPTION: ENFORCEMENT_OFF_CAPTION,
        STATUS_FRESHNESS_CAPTION: STATUS_FRESHNESS_CAPTION,
        failureLabel: failureLabel,
        modesFromProduct: modesFromProduct,
        defaultModes: defaultModes,
        stateFromVisibilityPayload: stateFromVisibilityPayload,
        deriveAutoBasis: deriveAutoBasis,
        deriveFailureCodes: deriveFailureCodes,
        selectID: selectID,
        badgeID: badgeID,
        failuresID: failuresID,
        effectiveForMode: effectiveForMode,
        badgeText: badgeText,
        showFailures: showFailures,
        escapeHTML: escapeHTML,
        renderOptionsHTML: renderOptionsHTML,
        renderRowHTML: renderRowHTML,
        renderSectionCaptionsHTML: renderSectionCaptionsHTML,
        renderSectionHTML: renderSectionHTML,
        collectModes: collectModes,
        bindAxisChange: bindAxisChange,
        bindStatusChange: bindStatusChange,
        refreshLivePreview: refreshLivePreview,
        updateRowFromMode: updateRowFromMode
    };

    root.ShopandaProductVisibilitySection = api;
    if (typeof module !== "undefined" && module.exports) {
        module.exports = api;
    }
})(typeof globalThis !== "undefined" ? globalThis : this);
