# Phase 12 PR Specs

| PR | Track | Status | Spec |
| --- | --- | --- | --- |
| PR-1048 | — | planned | [Allow zero-price products](PR-1048.md) |
| PR-1049 | A | planned | [`Type` field + migration](PR-1049.md) |
| PR-1050 | A | planned | [Type-driven shipping/tax requirements](PR-1050.md) |
| PR-1051 | A | planned | [Admin API type field support](PR-1051.md) |
| PR-1052 | A | planned | [CSV import/export: type field](PR-1052.md) |
| PR-1053 | A | planned | [Admin GUI: type selector + type-specific sections](PR-1053.md) |
| PR-1054 | B | planned | [Enforce `Status = active` in core read paths](PR-1054.md) |
| PR-1055 | B | planned | [Four-axis visibility model](PR-1055.md) |
| PR-1056 | B | planned | [Admin vs. public/storefront API split](PR-1056.md) |
| PR-1057 | B | planned | [Enforce visibility in storefront read paths](PR-1057.md) |
| PR-1058 | B | planned | [Cart/checkout purchasability gate + hook point](PR-1058.md) |
| PR-1059 | B | planned | [Visibility admin GUI](PR-1059.md) |
| PR-1060 | C | planned | [Bundle domain model](PR-1060.md) |
| PR-1061 | C | planned | [Grouped domain model](PR-1061.md) |
| PR-1062 | C | planned | [Bundle/grouped pricing rollup](PR-1062.md) |
| PR-1063 | C | planned | [Stock rollup per type](PR-1063.md) |
| PR-1064 | C | planned | [Order line explosion + returns integration](PR-1064.md) |
| PR-1065 | C | planned | [Omnibus/VAT per component line](PR-1065.md) |
| PR-1066 | C | planned | [Bundle/grouped admin API + CSV + GUI](PR-1066.md) |
| PR-1067 | D | planned | [`LinkedProduct` catalog eligibility](PR-1067.md) |
| PR-1068 | D | planned | [`RequiresLinkedParent` purchasability gate](PR-1068.md) |
| PR-1069 | D | planned | [`ProductAssignment` instance domain](PR-1069.md) |
| PR-1070 | D | planned | [Assignment flow: cart-time + post-purchase](PR-1070.md) |
| PR-1071 | D | planned | [Admin API for assignment](PR-1071.md) |
| PR-1072 | D | planned | [Returns integration for assigned children](PR-1072.md) |
| PR-1073 | D | planned | [CSV import/export + admin GUI](PR-1073.md) |
| PR-1074 | E | planned | [`Downloadable` type + file asset attachment](PR-1074.md) |
| PR-1075 | E | planned | [`DownloadGrant` domain](PR-1075.md) |
| PR-1076 | E | planned | [Digital-goods VAT/tax category](PR-1076.md) |
| PR-1077 | E | planned | [Storefront download delivery endpoint](PR-1077.md) |
| PR-1078 | E | planned | [Admin API + CSV + GUI for downloadable management](PR-1078.md) |
| PR-1079 | F | planned | [Search index schema extension](PR-1079.md) |
| PR-1080 | F | planned | [On-save reindex trigger generalization](PR-1080.md) |
| PR-1081 | F | planned | [CSV import/export final consistency pass](PR-1081.md) |
| PR-1082 | F | planned | [GraphQL read parity](PR-1082.md) |
| PR-1083 | F | planned | [RUNBOOK.md + admin GUI consolidation pass](PR-1083.md) |

Roadmap: [`../ROADMAP.md`](../ROADMAP.md)

**Ordering rule:** PR-1048 ships standalone, first. Track A (PR-1049–1053) before everything else — `Type` is referenced by every later track. Track B (PR-1054–1059) before Tracks C/D/E — bundle, linked-child, and downloadable salability rules are all built on `Purchasable`. Tracks C, D, and E have no dependency on each other. Track F (PR-1079–1083) depends on C/D/E being substantially complete and on Phase 11 Track B (search indexing) already having shipped.

Continues Phase 11's PR numbering (highest prior: PR-1047).
