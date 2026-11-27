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
        var localeExtra = "";
        if (canWrite) {
            localeExtra =
                '<label>Add locale<input name="new_locale" id="email-template-new-locale" placeholder="e.g. de"></label>' +
                '<button type="button" id="email-template-add-locale" class="secondary">Use locale</button>';
        }
        var actions = "";
        if (canWrite) {
            actions =
                '<div style="margin-top:1rem" id="email-template-write-actions">' +
                '<button type="submit" id="email-template-save">Save</button> ' +
                '<button type="button" id="email-template-revert" class="danger">Revert to default</button>' +
                "</div>" +
                '<fieldset style="margin-top:1.5rem" id="email-template-test-fieldset"><legend>Test send</legend>' +
                '<label>To<input type="email" name="test_to" id="email-template-test-to" placeholder="you@example.com"></label>' +
                '<button type="button" id="email-template-test-send">Send test</button>' +
                '<p class="admin-form-hint">Sends the <strong>saved</strong> render for this locale (not unsaved textarea edits). Subject is prefixed with [TEST]. Limited to one send per 30 seconds.</p>' +
                "</fieldset>";
        } else if (opts.templateWritable && !opts.hasContentWrite) {
            actions = '<p class="admin-form-hint" id="email-template-readonly-perm">Your role can view templates but not save, revert, or test-send.</p>';
        } else if (!opts.templateWritable) {
            actions = '<p class="admin-form-hint" id="email-template-readonly-name">View only (not overridable).</p>';
        }
        return banner +
            '<form id="email-template-form">' +
            '<label>Locale<select name="locale" id="email-template-locale">' +
            buildLocaleOptionsHTML(localeOptions, selected, escText) +
            "</select></label>" +
            localeExtra +
            meta +
            '<label>Subject<input name="subject" id="email-template-subject" value=""' + disabled + " required></label>" +
            '<label>Body (HTML)<textarea name="body" id="email-template-body" rows="16"' + disabled + "></textarea></label>" +
            actions +
            "</form>" +
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
        ["saveBtn", "revertBtn", "testBtn", "addLocaleBtn", "localeSelect", "newLocaleInput"].forEach(function (key) {
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

    var api = {
        escAttr: escAttr,
        newLoadGate: newLoadGate,
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
