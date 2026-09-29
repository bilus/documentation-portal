# Ledger, plan: docs/plans/2026-09-29-try-it-switch.md

- 2026-09-29 plan drafted for bilus/documentation-portal#14 on bilus/development, from 851ea2a; questions Q1 to Q8, each with an assumption.
- 2026-09-29 decision: no metaphor (Q7), as for #2 and #13.
- 2026-09-29 vocabulary: added Try It console and Try It setting; extended flags, environment, configuration, startup, portal configuration and viewer page.
- 2026-09-29 design: docs/flow.dfd gains box 5, Add the Try It setting to the portal configuration (main.setTryIt), between boxes 4 and 3.
- 2026-09-29 inline changes: config.HideTryIt with -hide-try-it and DOCPORTAL_HIDE_TRY_IT in main.parseConfig (TestParseConfigReadsHideTryIt); portal.Config.HideTryIt through site and page to templates/viewer.html, which now shows the Try It console unless the setting hides it (TestViewerPageHidesTryIt, and TestViewerTryIt in the browser); TestStartupShowsTryIt is the smoke test through box 5.
- 2026-09-29 test change proposed for the gate (Q6): TestViewerPage no longer expects hideTryIt="true" on the default viewer page, and checks that the page has no hideTryIt.
- 2026-09-29 holes added: 1 main.setTryIt. Acceptance test written and skipped: 1 TestStartupHidesTryIt.
- 2026-09-29 the Agent tool of this session sets no reasoning effort, so both reviews run at the session's default.
- 2026-09-29 design: docs/flow.3.dfd's entity is now box 5, the new predecessor of process 3.
- 2026-09-29 decision (user): no vocabulary review for this issue; the running review was stopped before it reported.
- 2026-09-29 design review, round 1 (sub-agent), 5 findings, no rule of design.md broken. Applied: 1 (Q5 names box 4 and states the cost of box 5), 3 (portal.Config's doc comment names the Try It setting), 4 (requirement 1 cites the reversal of #2's decision D4), 5 (startup's doc comment rewrapped). Carried, for the handoff: the Try It setting past boxes 2 and 4, the address past boxes 2, 4, 5 and 3, the content directory name past box 2, and the handles and the spec path through box 5.
- 2026-09-29 decision (user): the design review and the vocabulary review run once, on the complete change after the last stage, before the pull request opens; not at the plan gate or at stage boundaries. This gate's design review had already run.
- 2026-09-29 plan approved by the user, with the change to TestViewerPage (Q6). Signatures, top diagram and vocabulary frozen; score card cached.
