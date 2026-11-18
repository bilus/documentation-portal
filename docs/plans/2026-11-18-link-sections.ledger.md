# Ledger — plan: docs/plans/2026-11-18-link-sections.md

- 2026-11-18. Design agreed in conversation: a section of type `link` with a `url`, shown in the navigation bar; the program serves the page and draws its own bar. Discarded: a page section that renders a template of the custom templates, and a section backed by a handler the program registers.
- 2026-11-18. Decisions: the key is `url`; the href carries no base path; a link section may come first; title and slug rules stay as for every section.
- 2026-11-18. Plan written, waiting for approval.
- 2026-11-18. Plan approved. The user then asked to implement, test, commit and push without the halts, and to tag the version: stages 1 and 2 land as one commit.
- 2026-11-18. Stage 1 started and complete, uncommitted: `LinkSection`, `Section.URL`, `section.link`, and the holes `2 portal.linkSection`, `2 portal.linkURL` and `2 portal.section.pageURL`. Plan diff: the hole in `openSection` became the function `linkSection`, since the link case has to run before the check of the input. Build and `go test ./portal/` green; census 3.
- 2026-11-18. Holes filled: `linkURL`, `linkSection`, `pageURL`; `openSection` refuses a `url` on a spec or docs section. Census 0.
- 2026-11-18. Decision: `linkURL` checks the escaped path. A probe showed that `////example.com` parses to a path that `URL.String` writes back as `//example.com`, a link to another host, and that a check of the decoded path would refuse `/%2f/x`. `/\example.com` comes back as `/%5Cexample.com`.
- 2026-11-18. Plan diff: the test of `linkURL` is `TestLinkSectionURL`, since `TestLinkURL` exists for toc links. Requirement 7's second half, that no document link resolves to a link section, has no test of its own: `contentPath` and `specPart` skip every section that is not a docs or a spec section.
- 2026-11-18. Finding: `TestOneQuestionLimitForAReaderInTheBrowser` of the example application timed out in 3 of 25 runs on the branch and in 0 of 17 on master. The demo configuration has no link section and the change touches no code of the chat or its limit; the 12 last runs on the branch passed. Not explained; left as it is.
- 2026-11-18. Stage 2 complete: `devbox run make build lint test test-e2e` green, census 0. Deferred: the defect review of AGENTS.md, which waits for a pull request.
