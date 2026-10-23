# Phase 13 PR Specs

| PR | Track | Status | Spec |
| --- | --- | --- | --- |
| PR-1084 | A | planned | [`RunPluginFS` + lock + namespaced versions](PR-1084.md) |
| PR-1085 | A | planned | [Move B2B onto `RunPluginFS`](PR-1085.md) |
| PR-1086 | A | planned | [`RegisterMigrations` + `app migrate`](PR-1086.md) |
| PR-1087 | B | planned | [`manual` trigger + `Enqueue` + queue bind](PR-1087.md) |
| PR-1088 | B | planned | [SDK + docs + example](PR-1088.md) |

Roadmap: [`../ROADMAP.md`](../ROADMAP.md)

**Ordering rule:** Track A (1084→1085→1086) before Track B in calendar time (site-ready: plugin tables first). Track B has no code dependency on A. Do not start a merchant site until all five are `done`.

Continues Phase 12 numbering (highest reserved: PR-1083).
