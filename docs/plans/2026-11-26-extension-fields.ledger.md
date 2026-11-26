# Ledger, plan: docs/plans/2026-11-26-extension-fields.md

- 2026-11-26. Design agreed in conversation. Discarded: `Portal[Ext any]` and `Config[Ext any]`, since the type parameter would reach `Access`, `Library`, `New`, the chat package and every embedding program, and `Everything` and `AccessOf` have no place for it. Decided by the user: a plain `Portal` with `Ext`, and a method `GetExt` in place of a function `ExtOf`.
- 2026-11-26. Stage 1 started from `6b5f3b2`. A probe showed that Go 1.27 compiles a generic method, so `GetExt` is `GetExt[T any]() (T, error)` (Q1, an assumption).
- 2026-11-26. No diagram changes. Vocabulary: extension fields added.
- 2026-11-26. Holes added: `2 portal.ReadConfig`, `2 portal.Portal.GetExt`.
- 2026-11-26. Skeleton complete, uncommitted, waiting for the plan's approval.
- 2026-11-26. Plan approved, with `GetExt` as a generic method. Score card cached.
