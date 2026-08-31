# Phase 11 PR Specs

| PR | Track | Status | Spec |
| --- | --- | --- | --- |
| PR-1027 | — | done | [Fix reservation expiry gap](PR-1027.md) |
| PR-1028 | A | done | [Job introspection](PR-1028.md) |
| PR-1029 | A | done | [Jobs admin API](PR-1029.md) |
| PR-1030 | A | done | [Scheduler admin](PR-1030.md) |
| PR-1031 | A | planned | [Jobs + scheduler admin GUI](PR-1031.md) |
| PR-1032 | A | planned | [Jobs/scheduler CLI](PR-1032.md) |
| PR-1033 | B | planned | [Reindex as a job](PR-1033.md) |
| PR-1034 | B | planned | [Partial & scoped reindex](PR-1034.md) |
| PR-1035 | B | planned | [Reindex admin API + progress](PR-1035.md) |
| PR-1036 | B | planned | [On-save incremental indexing](PR-1036.md) |
| PR-1037 | B | planned | [Category indexing + relationship fix](PR-1037.md) |
| PR-1038 | B | planned | [Search admin GUI](PR-1038.md) |
| PR-1039 | C | planned | [Tag-based invalidation](PR-1039.md) |
| PR-1040 | C | planned | [In-memory (L1) cache tier](PR-1040.md) |
| PR-1041 | C | planned | [Rate limiting hardening](PR-1041.md) |
| PR-1042 | C | planned | [Cache admin API + CLI](PR-1042.md) |
| PR-1043 | C | planned | [Cache admin GUI](PR-1043.md) |
| PR-1044 | D | planned | [Full-page cache core + cacheability policy](PR-1044.md) |
| PR-1045 | D | planned | [Fragment mechanism (ESI-equivalent)](PR-1045.md) |
| PR-1046 | D | planned | [Invalidation wiring + stampede guard](PR-1046.md) |
| PR-1047 | D | planned | [Observability + rollup](PR-1047.md) |

Roadmap: [`../ROADMAP.md`](../ROADMAP.md)

**Ordering rule:** PR-1027 ships standalone, first. Track A (PR-1028–1032) before Track B and Track D — both reuse its job-introspection foundation. Track C (PR-1039–1043) before Track D — the full-page cache is built on tag invalidation and the L1 tier's invalidation-propagation story. Track B has no dependency on C/D and can run in parallel with either once Track A is done.

Continues Phase 10's PR numbering (highest prior: PR-1026).
