# Build the navigation from a Stoplight toc.json

Issue: bilus/documentation-portal#22.
Ledger: `docs/plans/2026-10-01-toc-navigation.ledger.md`.
Design: `docs/flow.dfd` and `docs/flow.3.dfd`, terms in `docs/vocabulary.md`, review page in `docs/review/index.html`.
Branch: `bilus/development`, which tracks `origin/master`. Base: `6b069d7`.

## Requirements

Restated from the issue, and optional at the user's request. The `toc.json` of `iBiquity/conrad-api-docs` holds 4 items and a group of 9: 11 entries link a markdown file, 1 the spec and 1 an external URL.

1. With a toc path, which the operator names with `-toc-path` or `DOCPORTAL_TOC_PATH` (Q1), the document sidebar shows the toc file's entries in its order, with its titles, and its groups as the sidebar's groups.
2. An entry for a markdown file opens its document page, an entry for the configured spec or one of its operations in Stoplight's form opens the viewer page, and an entry with an http or https URL opens that URL.
3. A markdown file missing from the toc file stays reachable by its URL and in the document list, but the sidebar omits it.
4. Without a toc path, the sidebar works as in #13.
5. A missing or invalid toc file does not break the portal: the sidebar falls back to the list of #13, and the log names the problem.
6. A toc path outside the documentation root stops docportal at startup, as a spec path outside it does.

Out of scope: the viewer page's sidebar, which Stoplight Elements draws (Q2), and caching the toc file.

## Questions and assumptions

- Q1. Optional. Assumption: the sidebar follows a toc file only when the operator names its path, relative to the documentation root, with `-toc-path` or `DOCPORTAL_TOC_PATH`; the path is empty by default, which keeps the sidebar of #13. For conrad: `-toc-path toc.json`. Against: Stoplight finds `toc.json` by its name, so an operator coming from Stoplight has to name it. Using the file whenever it exists would make it optional for the repository, not for the operator.
- Q2. The issue's open question, the spec's entry. Assumption: the document sidebar shows it, linking the viewer page; the viewer page keeps the sidebar that Elements draws.
- Q3. Entries that no page serves: a markdown file outside the content directory, a hidden or missing one, any other file of the documentation root, a path that leaves the root, and a URL with a scheme other than http and https, or with a host but no scheme. Assumption: the sidebar leaves them out.
- Q4. The toc file's form, from Stoplight: `{"items": [...]}`, each entry with a `type` of `item` (with a `title` and a `uri`), `group` (with a `title` and `items`) or `divider` (with a `title`). Assumption: the top-level items before the first group or divider form an untitled group; a group becomes a titled group; a divider starts a titled group for the top-level items after it; the entries of a group inside a group join the enclosing group in place. A file that is not JSON, has no `items`, or holds an entry of another type, without a title, or an item without a uri, is invalid.
- Q5. A `uri` resolves against the documentation root alone, as the issue's note asks: `docs/guide-oauth.md` and `/docs/guide-oauth.md` name the same file. The markdown file's directory plays no part, unlike a relative link in a document (#20).
- Q6. The current page: the sidebar marks an entry that links the current document page, as in #13.
- Q7. The document list at `/docs/` still lists every markdown file, with the sidebar of the toc file.
- Q8. Cost: each document page reads and parses the toc file and lists the markdown files once. Caching is out of scope.
- Q9. Design. The top diagram carries the toc path from box 1 to box 2, which pairs it with the documentation root, and box 3 refuses one outside the root. In `docs/flow.3.dfd`, box 3.1 checks it, and box 3.3's document pages lay out their sidebar from it. The work of reading the toc file happens per request, behind box 3.3, as for #13 and #20. The vocabulary changes flags, environment, configuration, portal configuration and document sidebar, and adds toc path and toc file.
- Q10. Existing tests change at this gate in their setup only: `TestOpenRootKeepsReadsInside` passes `main.openRoot` an empty toc path. The plan gate adds the toc path to the configuration inline, with its tests: the flag, the environment variable, `main.openRoot`, `portal.Config.TocPath` and the check in `portal.checkConfig`.
- Q11. Metaphor: none, as for #2, #13, #14, #19 and #20.
- Q12. Reviews: the defect review of AGENTS.md, once, before the pull request.
- Q13. Delivery: one pull request for the whole issue after the last stage's approval, with `Closes #22` at creation.

## The change in brief

The operator may now name a toc file, in Stoplight's `toc.json` form, with a toc path inside the documentation root. With one, the document sidebar shows the toc file's entries in its order and under its titles: a markdown file's entry links its document page, the spec's entry links the viewer page, and an http or https URL stays as it is. The sidebar leaves out an entry that no page serves, and falls back to the list of markdown files, with a line in the log, when the toc file is missing or invalid. Without a toc path, the sidebar stays as it is.

## Stages

Each stage ends with `devbox run make build lint test test-e2e`, the hole census (`grep -rn "HOLE(" --include='*.go' .`), the score card, the review page and a halt for review.

The plan gate adds the toc path to the configuration inline, with its tests, and declares the toc functions as holes whose mock data give an empty sidebar when a toc path is set. No existing test sets one, so every existing test stays green.

### Stage 1: the sidebar from the toc file

- Goal: with a toc path, each document page's sidebar shows the toc file's entries, linked as Q2 to Q6 say, and falls back to the list of markdown files, with a line in the log, when the toc file is missing or invalid.
- Requirement: 1, 2, 3 and 5.
- Dependencies: none.
- Holes: `1 portal.readToc`, `1 portal.site.tocGroups`, `1 portal.site.tocLink`.
- Acceptance: `TestTocSidebar`, `TestTocSidebarFallsBack`, and `TestDocPageSidebarFollowsToc` through the HTTP handler.
- Size: 200 lines.
