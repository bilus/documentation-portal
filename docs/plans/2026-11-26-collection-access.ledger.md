# Ledger, plan: docs/plans/2026-11-26-collection-access.md

- 2026-11-26. Requested by the user: extension fields on a collection, an access hook that decides whether a collection is visible, with the filter in code and not in the nav and home templates, and a test with `x-roles` on portals and collections for readers of two roles.
- 2026-11-26. Stage 1 started from `bb158a7`. Assumptions recorded in the plan: an optional `CollectionAccess` in the form of `PreviewAccess` (Q1), and a hidden collection closes the list, not the pages (Q2).
- 2026-11-26. Design: box 3.4.2 of `docs/flow.3.4.dfd` now asks the embedding program about each collection too; the type `collection` added. No other diagram changes.
- 2026-11-26. Vocabulary: collection access added; extension fields, collection, listed portal, access and configuration file changed.
- 2026-11-26. Holes added: `2 portal.ReadConfig`, `2 portal.Collection.Clone`, `2 portal.Collection.GetExt`, `3 portal.showCollections`.
- 2026-11-26. The skill's folder `~/.claude/skills/hole-driven-delivery` is gone from this machine, so its `design.md` and `review-page.md` were not read again; the installed dfd, dfdmetrics and dfdreview ran as before. Reviews follow AGENTS.md.
- 2026-11-26. Skeleton complete, uncommitted: `GOFLAGS=-count=1 devbox run make build lint test test-e2e` passes; the census holds 4 holes and 7 skipped tests; the score card is unchanged (live_range 9, read_distance 3, top_shared 0, shared 3, pressure 1, package_spread 0, findings 0). The review page compares with `bb158a7`. Waiting for the plan's approval.
- 2026-11-26. Plan approved, with `CollectionAccess` as an optional interface (Q1), a hidden collection closing the list only (Q2), and the removal of one case of `TestReadConfigStillRefusesOtherKeys` (Q4). Score card cached.
- 2026-11-26. Stage 2 started from `624be75`.
- 2026-11-26. Holes filled: `2 portal.ReadConfig`, `2 portal.Collection.Clone`, `2 portal.Collection.GetExt`, with the unexported decoder `decodeExt` that `Portal.GetExt` now shares; three tests unskipped, unchanged; the collection case removed from `TestReadConfigStillRefusesOtherKeys` (Q4).
- 2026-11-26. Stage 2 complete, uncommitted at the user's word: `GOFLAGS=-count=1 devbox run make build lint test test-e2e` passes; the census holds 1 hole, `3 portal.showCollections`, and 4 skipped tests; score unchanged. The review page compares with `624be75`. Waiting for review.
- 2026-11-26. Stage 2 approved. Score card cached.
