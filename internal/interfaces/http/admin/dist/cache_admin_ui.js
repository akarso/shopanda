// cache_admin_ui.js — Cache admin clear-form helpers (PR-1043).
// Loaded before admin.js in the SPA; also CommonJS-exported for Node tests.
(function (root) {
    var CONFIRM = "CLEAR ALL";

    function isAllConfirmed(value) {
        return String(value || "").trim() === CONFIRM;
    }

    function modeOptions(canWrite, canClearAll) {
        var opts = [];
        if (canWrite) {
            opts.push("prefix", "tag", "key");
        }
        if (canClearAll) {
            opts.push("all");
        }
        return opts;
    }

    function submitDisabledForMode(mode, confirmValue) {
        return mode === "all" && !isAllConfirmed(confirmValue);
    }

    function buildPayload(mode, value) {
        if (mode === "all") {
            return { ok: true, payload: { all: true } };
        }
        var raw = String(value || "");
        var trimmed = raw.trim();
        if (!trimmed) {
            return { ok: false, error: "empty" };
        }
        var payload = {};
        // Keys are stored literally (including boundary whitespace); prefix/tag trim.
        payload[mode] = mode === "key" ? raw : trimmed;
        return { ok: true, payload: payload };
    }

    function normalizeL1(l1) {
        if (l1 == null) {
            return [];
        }
        if (!Array.isArray(l1)) {
            return null;
        }
        return l1;
    }

    function setAriaDisabled(el, disabled) {
        if (!el) {
            return;
        }
        el.disabled = disabled;
        if (typeof el.setAttribute === "function") {
            el.setAttribute("aria-disabled", disabled ? "true" : "false");
        }
    }

    function applyModeVisibility(els, mode) {
        var isAll = mode === "all";
        els.valueField.hidden = isAll;
        els.confirmBox.hidden = !isAll;
        if (!isAll) {
            els.confirmInput.value = "";
        }
        setAriaDisabled(els.submitBtn, submitDisabledForMode(mode, els.confirmInput.value));
        if (typeof els.submitBtn.setAttribute === "function") {
            if (isAll) {
                els.submitBtn.setAttribute("aria-describedby", "cache-clear-all-help");
            } else {
                els.submitBtn.removeAttribute("aria-describedby");
            }
        }
    }

    function setFormLocked(els, locked) {
        setAriaDisabled(els.modeSelect, locked);
        setAriaDisabled(els.valueInput, locked);
        setAriaDisabled(els.confirmInput, locked);
    }

    // newStatsLoadGate ignores a stats response once a newer load has
    // started, so an in-flight refresh cannot overwrite post-clear counts.
    function newStatsLoadGate() {
        var seq = 0;
        return {
            start: function () {
                seq += 1;
                var id = seq;
                return {
                    isCurrent: function () {
                        return id === seq;
                    }
                };
            }
        };
    }

    // bindClearForm wires a duck-typed form. deps.withBusy matches
    // withButtonBusy(btn, label, startAction, afterRestore).
    function bindClearForm(els, deps) {
        function sync() {
            applyModeVisibility(els, els.modeSelect.value);
        }

        function afterBusy() {
            setFormLocked(els, false);
            sync();
        }

        els.modeSelect.addEventListener("change", sync);
        els.confirmInput.addEventListener("input", sync);
        sync();

        els.form.addEventListener("submit", function (e) {
            if (e && typeof e.preventDefault === "function") {
                e.preventDefault();
            }
            var mode = els.modeSelect.value;
            if (mode === "all" && !isAllConfirmed(els.confirmInput.value)) {
                deps.onMessage("error", "Type CLEAR ALL to confirm a full flush.");
                return;
            }
            var built = buildPayload(mode, els.valueInput.value);
            if (!built.ok) {
                deps.onMessage("error", "Enter a " + mode + " value.");
                return;
            }
            if (els.submitBtn.disabled) {
                return;
            }
            setFormLocked(els, true);
            deps.withBusy(els.submitBtn, "Clearing…", function () {
                return Promise.resolve(deps.clear(built.payload)).then(function (result) {
                    if (result && result.error) {
                        deps.onMessage("error", result.error);
                        return;
                    }
                    deps.onMessage("ok", result && result.message ? result.message : "Cleared");
                    if (mode === "all") {
                        els.confirmInput.value = "";
                    } else {
                        els.valueInput.value = "";
                    }
                    if (typeof deps.loadStats === "function") {
                        return deps.loadStats();
                    }
                }).catch(function (err) {
                    var msg = (err && err.message) ? err.message : "Clear failed.";
                    deps.onMessage("error", msg);
                });
            }, afterBusy);
        });

        return { sync: sync, afterBusy: afterBusy };
    }

    var api = {
        CONFIRM: CONFIRM,
        isAllConfirmed: isAllConfirmed,
        modeOptions: modeOptions,
        submitDisabledForMode: submitDisabledForMode,
        buildPayload: buildPayload,
        normalizeL1: normalizeL1,
        applyModeVisibility: applyModeVisibility,
        setFormLocked: setFormLocked,
        bindClearForm: bindClearForm,
        newStatsLoadGate: newStatsLoadGate
    };

    root.ShopandaCacheAdminUI = api;
    if (typeof module !== "undefined" && module.exports) {
        module.exports = api;
    }
})(typeof globalThis !== "undefined" ? globalThis : this);
