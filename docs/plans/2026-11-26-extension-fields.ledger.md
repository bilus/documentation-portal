# Ledger, plan: docs/plans/2026-11-26-extension-fields.md

- 2026-11-26. Design agreed in conversation. Discarded: `Portal[Ext any]` and `Config[Ext any]`, since the type parameter would reach `Access`, `Library`, `New`, the chat package and every embedding program, and `Everything` and `AccessOf` have no place for it. Decided by the user: a plain `Portal` with `Ext`, and a method `GetExt` in place of a function `ExtOf`.
- 2026-11-26. Stage 1 started from `6b5f3b2`. A probe showed that Go 1.27 compiles a generic method, so `GetExt` is `GetExt[T any]() (T, error)` (Q1, an assumption).
- 2026-11-26. No diagram changes. Vocabulary: extension fields added.
- 2026-11-26. Holes added: `2 portal.ReadConfig`, `2 portal.Portal.GetExt`.
- 2026-11-26. Skeleton complete, uncommitted, waiting for the plan's approval.
- 2026-11-26. Plan approved, with `GetExt` as a generic method. Score card cached.
- 2026-11-26. Stage 2 started from `9f0fd64`.
- 2026-11-26. Holes filled: `2 portal.ReadConfig`, `2 portal.Portal.GetExt`, with the unexported helpers `extensionFields` and `plainValue`; five tests unskipped, unchanged. README section added.
- 2026-11-26. Stage 2 complete: `make build lint test test-e2e` green, census 0, score unchanged. Waiting for approval; the defect review follows.
- 2026-11-26. The series under this change was rebuilt for the review fixes of pull requests 46 and 47: the two commits of this plan were replayed unchanged onto `f29a2fc`, as `9a61816` and `16025d7`. The earlier tips are kept as the branch `backup/before-review-fixes`.
- 2026-11-26. Stage 2 approved. Defect review of AGENTS.md started on `f29a2fc..HEAD`.
- 2026-11-26. Defect review reported: 271 s, 126,073 tokens, 7 tool calls, 8 findings, one high. Wrong implementations on a copy of the tip (script: scratchpad `mutate3.py`, three runs each): 5 of 8 pass every test (partial-on-error, no-list-recursion, no-map-recursion, ext-on-link-portals-only, first-key-alone-checked); 3 are caught.
- 2026-11-26. Finding 1, high, confirmed by my own experiment: `x-f: .nan` and `x-f: .inf` pass ReadConfig, json.Marshal refuses them, chat's `digest` drops the error, and three different portals got the digest of no bytes, `e3b0c442...`. A stored page access then no longer closes when such a portal changes.
- 2026-11-26. Decisions proposed, waiting for approval. Apply: 1 (refuse NaN and infinities, and make digest fail safe); 2 by a deep copy of `Ext` in `openPortals`, with the hook's portals documented as read-only; 4 (a panic of the encoder becomes an error); 6 (all refused keys, sorted). Document: 3 and 5 (how YAML values convert). Discard: 7 (every digest changes once at upgrade), since the link portals change already adds `Type` and `URL` to the digest's input; 8, informational.
