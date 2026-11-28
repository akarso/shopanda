// email_templates_admin_ui.js — Email template editor helpers (PR-1214 CR).
// Load gates + attribute escaping + write-control rules. No DOM required for tests.
(function (root) {
    "use strict";

    // escAttr encodes for HTML attribute values (quotes included).
    // Prefer setting input.value / textarea.value / option.value via DOM properties.
    function escAttr(s) {
        return String(s == null ? "" : s)
            .replace(/&/g, "&amp;")
            .replace(/"/g, "&quot;")
            .replace(/</g, "&lt;")
            .replace(/>/g, "&gt;");
    }

    // newLoadGate drops stale GET/preview callbacks after a newer load starts.
    function newLoadGate() {
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
            },
            current: function () {
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

    function formatLocaleLabel(locale) {
        if (locale === "" || locale == null) {
            return "default";
        }
        return String(locale);
    }

    // normalizeLocaleInput mirrors ParseTemplateLocale primary-subtag rules for UI:
    // trim, take BCP 47 primary (before '-'/'_'), lower-case; reject empty/"und".
    function normalizeLocaleInput(raw) {
        var s = String(raw == null ? "" : raw).trim();
        if (s === "") {
            return { ok: true, locale: "" };
        }
        var primary = s.split(/[-_]/)[0].toLowerCase();
        if (!/^[a-z]{2,8}$/.test(primary) || primary === "und") {
            return { ok: false, error: "invalid locale" };
        }
        return { ok: true, locale: primary };
    }

    function canShowWriteControls(templateWritable, hasContentWrite) {
        return !!templateWritable && !!hasContentWrite;
    }

    function baselineFromRow(row) {
        row = row || {};
        return {
            locale: row.locale != null ? String(row.locale) : "",
            subject: row.subject != null ? String(row.subject) : "",
            body: row.body != null ? String(row.body) : "",
            source: row.source != null ? String(row.source) : ""
        };
    }

    function formMatchesBaseline(subject, body, locale, baseline) {
        baseline = baseline || { subject: "", body: "", locale: "" };
        return String(subject || "") === String(baseline.subject || "") &&
            String(body || "") === String(baseline.body || "") &&
            String(locale == null ? "" : locale) === String(baseline.locale || "");
    }

    function defaultStoreBannerNeeded(scopeStoreID, defaultStoreID) {
        var scope = String(scopeStoreID || "");
        var def = String(defaultStoreID || "");
        return scope !== "" && def !== "" && scope !== def;
    }

    function listLoadResult(listBody) {
        if (listBody && listBody.error) {
            return {
                ok: false,
                forbidden: listBody.error.code === "forbidden",
                message: (listBody.error && listBody.error.message) || "Failed to load email templates."
            };
        }
        if (!listBody || !Array.isArray(listBody.data)) {
            return { ok: false, forbidden: false, message: "Failed to load email templates." };
        }
        return { ok: true, items: listBody.data };
    }

    function findListMeta(items, name) {
        if (!Array.isArray(items)) {
            return null;
        }
        for (var i = 0; i < items.length; i++) {
            if (items[i] && items[i].name === name) {
                return items[i];
            }
        }
        return null;
    }

    // buildLocaleOptionsHTML builds <option> tags. Values use escAttr; labels use text escaping by caller via escAttr for safety in text nodes too.
    function buildLocaleOptionsHTML(localeOptions, selected, escText) {
        var encodeText = typeof escText === "function" ? escText : escAttr;
        var html = "";
        var opts = Array.isArray(localeOptions) ? localeOptions : [];
        for (var i = 0; i < opts.length; i++) {
            var loc = opts[i] == null ? "" : String(opts[i]);
            html += '<option value="' + escAttr(loc) + '"' +
                (loc === selected ? " selected" : "") + ">" +
                encodeText(formatLocaleLabel(loc)) + "</option>";
        }
        return html;
    }

    // editorShellHTML leaves subject/body empty — apply via DOM .value after insert.
    function editorShellHTML(opts) {
        opts = opts || {};
        var canWrite = !!opts.canWrite;
        var selected = opts.selectedLocale != null ? String(opts.selectedLocale) : "";
        var localeOptions = Array.isArray(opts.localeOptions) ? opts.localeOptions : [""];
        var escText = typeof opts.escText === "function" ? opts.escText : escAttr;
        var meta = opts.metaHTML || "";
        var banner = opts.bannerHTML || "";
        var disabled = canWrite ? "" : " disabled";
        // Add-locale and test-send stay outside #email-template-form so Enter in those
        // fields cannot implicitly submit Save.
        var localeBar =
            '<div id="email-template-locale-bar">' +
            '<label>Locale<select name="locale" id="email-template-locale">' +
            buildLocaleOptionsHTML(localeOptions, selected, escText) +
            "</select></label>";
        if (canWrite) {
            localeBar +=
                '<label>Add locale<input id="email-template-new-locale" placeholder="e.g. de" autocomplete="off"></label>' +
                '<button type="button" id="email-template-add-locale" class="secondary">Use locale</button>';
        }
        localeBar += "</div>";

        var writeActions = "";
        if (canWrite) {
            writeActions =
                '<div style="margin-top:1rem" id="email-template-write-actions">' +
                '<button type="submit" id="email-template-save">Save</button> ' +
                '<button type="button" id="email-template-revert" class="danger">Revert to default</button>' +
                "</div>";
        } else if (opts.templateWritable && !opts.hasContentWrite) {
            writeActions = '<p class="admin-form-hint" id="email-template-readonly-perm">Your role can view templates but not save, revert, or test-send.</p>';
        } else if (!opts.templateWritable) {
            writeActions = '<p class="admin-form-hint" id="email-template-readonly-name">View only (not overridable).</p>';
        }

        var testBlock = "";
        if (canWrite) {
            testBlock =
                '<fieldset style="margin-top:1.5rem" id="email-template-test-fieldset"><legend>Test send</legend>' +
                '<label>To<input type="email" id="email-template-test-to" placeholder="you@example.com" autocomplete="off"></label>' +
                '<button type="button" id="email-template-test-send">Send test</button>' +
                '<p class="admin-form-hint">Sends the <strong>saved</strong> render for this locale (not unsaved textarea edits). Subject is prefixed with [TEST]. Limited to one send per 30 seconds.</p>' +
                "</fieldset>";
        }

        return banner +
            localeBar +
            meta +
            '<form id="email-template-form">' +
            '<label>Subject<input name="subject" id="email-template-subject" value=""' + disabled + " required></label>" +
            '<label>Body (HTML)<textarea name="body" id="email-template-body" rows="16"' + disabled + "></textarea></label>" +
            writeActions +
            "</form>" +
            testBlock +
            '<section style="margin-top:2rem">' +
            "<h3>Preview</h3>" +
            '<p class="admin-form-hint">Uses the live render chain (requested locale → default override → file). Saved content only.</p>' +
            '<button type="button" id="email-template-preview-btn">Refresh preview</button>' +
            '<div id="email-template-preview-meta" class="admin-form-hint" style="margin-top:0.5rem"></div>' +
            '<p id="email-template-preview-subject" style="font-weight:600;margin:0.75rem 0 0.25rem"></p>' +
            '<iframe id="email-template-preview-frame" title="Email preview" sandbox="" referrerpolicy="no-referrer" style="width:100%;min-height:420px;border:1px solid #ccc;background:#fff"></iframe>' +
            "</section>";
    }

    function applyRowToFields(fields, row) {
        fields = fields || {};
        row = row || {};
        var locale = row.locale != null ? String(row.locale) : "";
        var subject = row.subject != null ? String(row.subject) : "";
        var body = row.body != null ? String(row.body) : "";
        if (fields.localeSelect) {
            fields.localeSelect.value = locale;
        }
        if (fields.subjectInput) {
            fields.subjectInput.value = subject;
        }
        if (fields.bodyInput) {
            fields.bodyInput.value = body;
        }
        return baselineFromRow({ locale: locale, subject: subject, body: body, source: row.source });
    }

    function setWriteControlsLocked(fields, locked) {
        fields = fields || {};
        var disabled = !!locked;
        ["saveBtn", "revertBtn", "testBtn", "addLocaleBtn", "localeSelect", "newLocaleInput", "testToInput"].forEach(function (key) {
            if (fields[key]) {
                fields[key].disabled = disabled;
            }
        });
        // Subject/body stay editable while locked only if we want merchants to keep typing —
        // CR says disable Save/Revert/Test while locale load in flight; keep fields editable
        // would risk confusion. Lock subject/body too during locale load.
        if (fields.subjectInput && fields.subjectInput.dataset && fields.subjectInput.dataset.ro !== "1") {
            fields.subjectInput.disabled = disabled;
        }
        if (fields.bodyInput && fields.bodyInput.dataset && fields.bodyInput.dataset.ro !== "1") {
            fields.bodyInput.disabled = disabled;
        }
    }

    function setTestSendEnabled(testBtn, enabled) {
        if (!testBtn) {
            return;
        }
        testBtn.disabled = !enabled;
    }

    function paintPreview(targets, data, ticket) {
        if (ticket && !ticket.isCurrent()) {
            return false;
        }
        targets = targets || {};
        data = data || {};
        var bits = [
            "source=" + (data.source || ""),
            "requested=" + formatLocaleLabel(data.requested_locale),
            "resolved=" + formatLocaleLabel(data.resolved_locale)
        ];
        if (data.fallback) {
            bits.push("fallback");
        }
        if (data.attachment_omitted) {
            bits.push("attachment omitted: " + data.attachment_omitted);
        }
        if (targets.meta) {
            targets.meta.textContent = bits.join(" · ");
        }
        if (targets.subject) {
            targets.subject.textContent = data.subject || "";
        }
        if (targets.frame) {
            targets.frame.setAttribute("srcdoc", data.body || "<p></p>");
        }
        return true;
    }

    function revertMessage(hadDbOverride) {
        if (hadDbOverride) {
            return "Reverted to default.";
        }
        return "No override for this locale (already file/builtin).";
    }

    // createScopedEditorSession owns separate locale / preview / write gates and
    // scopes queries to the captured editor root (never document.getElementById).
    // Preview refreshes must not call beginLocaleLoad / localeGate.start().
    function createScopedEditorSession(editor) {
        var localeGate = newLoadGate();
        var previewGate = newLoadGate();
        var writeGate = newLoadGate();
        var sideGate = newLoadGate(); // test-send (and similar) — not the write lock
        var localeLockOwner = null;

        function alive() {
            return !!(editor && editor.isConnected);
        }

        function q(id) {
            if (!editor || typeof editor.querySelector !== "function") {
                return null;
            }
            return editor.querySelector("#" + id);
        }

        function beginLocaleLoad() {
            writeGate.start(); // drop in-flight PUT/DELETE apply
            sideGate.start();  // drop in-flight test-send status paint
            var ticket = localeGate.start();
            localeLockOwner = ticket;
            return ticket;
        }

        // settleLocaleLoad: 'apply' | 'stale-unlock' | 'stale-ignore' | 'detached'
        function settleLocaleLoad(ticket) {
            if (!alive()) {
                if (localeLockOwner === ticket) {
                    localeLockOwner = null;
                }
                return "detached";
            }
            if (!ticket || !ticket.isCurrent()) {
                if (localeLockOwner === ticket) {
                    localeLockOwner = null;
                    return "stale-unlock";
                }
                return "stale-ignore";
            }
            localeLockOwner = null;
            return "apply";
        }

        function beginPreview() {
            return previewGate.start();
        }

        function settlePreview(ticket) {
            if (!alive()) {
                return "detached";
            }
            if (!ticket || !ticket.isCurrent()) {
                return "stale";
            }
            return "apply";
        }

        function beginSideAction() {
            return sideGate.start();
        }

        function settleSideAction(ticket) {
            if (!alive()) {
                return "detached";
            }
            if (!ticket || !ticket.isCurrent()) {
                return "stale";
            }
            return "apply";
        }

        function beginWrite() {
            sideGate.start(); // drop in-flight test-send status so it cannot overwrite "Saved."
            return writeGate.start(); // invalidate prior write apply
        }

        function settleWrite(ticket) {
            if (!alive()) {
                return "detached";
            }
            if (!ticket || !ticket.isCurrent()) {
                return "stale";
            }
            // A locale load started after this write → writeGate was bumped.
            return "apply";
        }

        function invalidate() {
            localeGate.start();
            previewGate.start();
            writeGate.start();
            sideGate.start();
            localeLockOwner = null;
        }

        return {
            editor: editor,
            alive: alive,
            q: q,
            beginLocaleLoad: beginLocaleLoad,
            settleLocaleLoad: settleLocaleLoad,
            beginPreview: beginPreview,
            settlePreview: settlePreview,
            beginSideAction: beginSideAction,
            settleSideAction: settleSideAction,
            beginWrite: beginWrite,
            settleWrite: settleWrite,
            invalidate: invalidate,
            // Exposed for tests.
            _localeLockOwner: function () { return localeLockOwner; }
        };
    }

    // sideActionCompletion tells the binder what to do after settleSideAction.
    // Stale (e.g. locale switched mid-SMTP) must still clear in-flight and refresh
    // the Send test button when the editor is connected; detached skips sync.
    function sideActionCompletion(outcome) {
        if (outcome === "detached") {
            return { clearInFlight: true, syncControls: false, paintStatus: false };
        }
        if (outcome === "stale") {
            return { clearInFlight: true, syncControls: true, paintStatus: false };
        }
        return { clearInFlight: true, syncControls: true, paintStatus: true };
    }

    // sideActionControlPlan is the unlock-vs-sync decision finishTestSend uses.
    // When locale/write still owns the lock, only refresh Send test — never unlockForm.
    function sideActionControlPlan(outcome, loadingLocale, writing) {
        var plan = sideActionCompletion(outcome);
        var ownerHoldsLock = !!(loadingLocale || writing);
        return {
            clearInFlight: plan.clearInFlight,
            unlockForm: plan.syncControls && !ownerHoldsLock,
            syncOnly: plan.syncControls && ownerHoldsLock,
            paintStatus: plan.paintStatus
        };
    }

    // formatTestSendLocaleLabel prefers resolved_locale when the send fell through.
    function formatTestSendLocaleLabel(sentLocale, data) {
        data = data || {};
        var sent = sentLocale == null ? "" : String(sentLocale);
        var resolved = data.resolved_locale != null ? String(data.resolved_locale) : sent;
        if (data.fallback || resolved !== sent) {
            return formatLocaleLabel(sent) + " → " + formatLocaleLabel(resolved) + " (fallback)";
        }
        return formatLocaleLabel(sent);
    }

    var api = {
        escAttr: escAttr,
        newLoadGate: newLoadGate,
        createScopedEditorSession: createScopedEditorSession,
        sideActionCompletion: sideActionCompletion,
        sideActionControlPlan: sideActionControlPlan,
        formatTestSendLocaleLabel: formatTestSendLocaleLabel,
        formatLocaleLabel: formatLocaleLabel,
        normalizeLocaleInput: normalizeLocaleInput,
        canShowWriteControls: canShowWriteControls,
        baselineFromRow: baselineFromRow,
        formMatchesBaseline: formMatchesBaseline,
        defaultStoreBannerNeeded: defaultStoreBannerNeeded,
        listLoadResult: listLoadResult,
        findListMeta: findListMeta,
        buildLocaleOptionsHTML: buildLocaleOptionsHTML,
        editorShellHTML: editorShellHTML,
        applyRowToFields: applyRowToFields,
        setWriteControlsLocked: setWriteControlsLocked,
        setTestSendEnabled: setTestSendEnabled,
        paintPreview: paintPreview,
        revertMessage: revertMessage
    };

    root.ShopandaEmailTemplatesAdminUI = api;
    if (typeof module !== "undefined" && module.exports) {
        module.exports = api;
    }
}(typeof globalThis !== "undefined" ? globalThis : this));
