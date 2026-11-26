# Ledger, plan: docs/plans/2026-11-26-link-portals.md

- 2026-11-26. Stage 1 started from `be2f627`. Plan written with ten questions. The user's requirement on order (a collection lists its portals in the order of the YAML) is requirement 4.
- 2026-11-26. No diagram changes: no new process or flow. Vocabulary: link portal added; portal and configuration file changed.
- 2026-11-26. Holes added: `2 portal.openPortals`, `2 portal.linkPortal`, `2 portal.site.url`, `2 portal.site.index`, `2 portal.site.visibleTo`, `2 portal.view.portalFor`, `3 portal.NewLibraries`, `3 portal.Library.Menu`, `3 chat.Chat.mount`. Inline, without a hole: `ReadConfig` copies `type` and `url`.
- 2026-11-26. The smoke test asserts the portals with sections alone: the skeleton cannot list a link portal before `visibleTo` is filled.
- 2026-11-26. Skeleton complete, uncommitted, waiting for the plan's approval.
- 2026-11-26. Plan approved with its assumptions. Score card cached.
