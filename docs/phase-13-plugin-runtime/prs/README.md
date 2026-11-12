# Phase 13 PR Specs

| PR | Track | Status | Spec |
| --- | --- | --- | --- |
| PR-1084 | A | done | [`RunPluginFS` + lock + namespaced versions](PR-1084.md) |
| PR-1085 | A | done | [Move B2B onto `RunPluginFS`](PR-1085.md) |
| PR-1086 | A | done | [`Migrator` + `app migrate`](PR-1086.md) |
| PR-1087 | B | done | [`manual` trigger + `Enqueue` + queue bind](PR-1087.md) |
| PR-1088 | B | done | [SDK + docs + example](PR-1088.md) |
| PR-1089 | B | done | [Sync job observability](PR-1089.md) |
| PR-1090 | C | done | [Local SignedURLStorage + BindMediaStorage](PR-1090.md) |
| PR-1091 | C | done | [Enforce Access.PluginOnlyWrite](PR-1091.md) |
| PR-1092 | C | planned | [Field ownership for plugin-only extension fields](PR-1092.md) |
| PR-1093 | C | planned | [Narrow port for core-internal extension operations](PR-1093.md) |
| PR-1094 | C | planned | [Retire plugin-only extension field definitions](PR-1094.md) |
| PR-1095 | C | planned | [Trust-propagation hardening and coverage gaps](PR-1095.md) |
| PR-1096 | C | planned | [Green `make check`: gofmt and go vet cleanup](PR-1096.md) |

Roadmap: [`../ROADMAP.md`](../ROADMAP.md)

**Ordering rule:** Track A (1084→1085→1086) before Track B in calendar time (site-ready: plugin tables first). Track B has no code dependency on A. Do not start a merchant site until Track A+B site-ready PRs are `done`. Track C (PR-1090) is additive media platform work for product plugins; not part of the original site-ready gate.

Continues Phase 12 numbering; highest issued in this phase: PR-1096.
