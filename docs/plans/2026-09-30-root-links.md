# Resolve markdown links against the documentation root

Issue: bilus/documentation-portal#20.
Ledger: `docs/plans/2026-09-30-root-links.ledger.md`.
Design: `docs/flow.dfd` and `docs/flow.3.dfd`, terms in `docs/vocabulary.md`, review page in `docs/review/index.html`.
Branch: `bilus/development`, which tracks `origin/master`. Base: `10d0c37`.

## Requirements

Restated from the issue. The guides of `iBiquity/conrad-api-docs` were written for Stoplight, which resolves a relative link against the project root. They hold 6 links to guides, such as `docs/guide-oauth.md`, 4 links to the spec, 2 of them to an operation as `CONRAD-Delivery-API.oas2.yml/paths/~1devices/post`, and 1 link with a leading `/`.

1. A relative link to a markdown file, written relative to the documentation root, opens the file's document page.
2. A link to the configured spec opens its viewer page, and a link to one of its operations in Stoplight's form, `<spec path>/paths/<path>/<method>` with the path escaped as in a JSON pointer, opens the viewer page at that operation.
3. A link with a leading `/`, such as `/docs/guide-oauth.md`, resolves against the documentation root too.
4. Fragment links and absolute URLs stay as they are.
5. A link that resolves outside the documentation root leads nowhere: the page shows its text without the link.
6. The operator names the documentation root, and the spec path and the content directory path are relative to it (Q1).
7. When no page serves the target of a relative link against the documentation root, and the link has no leading `/`, the target resolves against the markdown file's directory (Q2).
8. A relative link leads nowhere when no page serves its target (Q3).

Out of scope: images (#21), links inside raw HTML, links in the spec's own descriptions, rendered by Elements, and whether a fragment matches a heading ID that goldmark generates.

## Questions and assumptions

- Q1. The documentation root, the issue's first open question. Assumption: one new setting in place of two. `-root-dir` or `DOCPORTAL_ROOT_DIR` names the documentation root, `.` by default, and `-spec-path` and `-docs-path` (`DOCPORTAL_DOCS_PATH`) give the spec and the content directory as paths inside it. They replace `-specs-dir`, `DOCPORTAL_SPECS_DIR`, `-docs-dir` and `DOCPORTAL_DOCS_DIR`, and `portal.Config` takes one `Root` with `SpecPath` and `DocsPath` in place of `Specs` and `Docs`. The portal derives the content directory from the root, so no configuration can place it outside the root or name it twice. For conrad-api-docs: `-root-dir <clone> -spec-path CONRAD-Delivery-API.oas2.yml -docs-path docs`. For #4 the repository becomes the root, and #21 and #22 can read images and `toc.json` from it. Against: the flags change under the operator, with no alias for the old ones, since the operator is the only user. Keeping `-specs-dir` and `-docs-dir` and requiring the content directory inside the specs directory would keep the flags, but main would then compute the content directory's place from two local paths, where symlinks and letter case on macOS can defeat the comparison.
- Q2. A fallback to the file's own directory, the issue's second open question. Assumption: yes. A relative link without a leading `/` resolves against the documentation root first, as on Stoplight. When no page serves that target, it resolves against the markdown file's directory, as on GitHub. The sample documents link that way (`guide/intro.md`, `../README.md`), and so do most markdown files written outside Stoplight. Against: a link that is broken on Stoplight works in the portal, so the portal does not show its author the break.
- Q3. Targets inside the root that no page serves: a markdown file outside the content directory, an image, a directory, a missing file. Assumption: the link leads nowhere, where the browser shows a 404 today. A link to an image of the content directory could open its raw file, but the guides have none.
- Q4. Other parts of the spec. Assumption: a link to `<spec path>/<pointer>` that names no operation of the published spec opens the viewer page at its start: a schema, a missing operation or an unpublished one. The route comes from the published spec, so a link to an unpublished operation reveals no operationId.
- Q5. Routes. Elements 9.0.25 routes an operation to `#/operations/{operationId}`, and an operation without one to `#/paths/{slug}/{method}`. The slug replaces each `/`, `{`, `}` and space of the path with `-`, collapses the first run of dashes and trims a dash from each end. Checked in the viewer on the conrad spec: `#/operations/registerDevice` opens the operation behind `paths/~1devices/post`, `#/paths/apps-links-appLinkId/get` opens `GET /apps/links/{appLinkId}`, the one operation of 43 without an operationId, and an unknown route shows the overview. An upgrade of Elements can change the routes, so a browser test opens one.
- Q6. Cost. A document page that links into the spec parses the published spec once: 13 ms on the conrad spec. Each relative link lists the markdown files once. Caching is out of scope.
- Q7. Existing tests change at this gate, in their setup only: the flags and the environment in `cmd/docportal/main_test.go`, and the configuration in the e2e tests, `newPortal`, `newDocsPortal` and `TestCheckConfig`. `TestOpenSpecsKeepsReadsInside` and `TestOpenDocsKeepsReadsInside` merge into `TestOpenRootKeepsReadsInside`, since one function opens the root, and `TestStartupRejectsMissingDocsDir` expects its error from `portal.New`. No assertion about a page of the portal changes at this gate. Stage 1 changes one: `TestDocPage` expects `href="../README.md"` on `guide/intro.md`, and the link becomes `href="/docs/README.md"`, the same page, resolved by the portal instead of the browser. The sample `README.md` gains a Stoplight operation link for the browser test of stage 2.
- Q8. Design. The portal rewrites the links per request, in the document page's handler, so the work belongs to box 3.3, as decided for #2 and #13. The top diagram changes: box 2 opens the documentation root and takes both paths, and box 4 goes, since no step opens the content directory any more. Box 3.1 also checks the content directory path.
- Q9. Metaphor: none, as for #2, #13, #14 and #19.
- Q10. Reviews: the design and vocabulary reviews run once, before the pull request.
- Q11. Delivery: one pull request for the whole issue after the last stage's approval, with `Closes #20` at creation. It carries `10d0c37`, which adds git to devbox so that `gh` finds it.

## The change in brief

The operator now names a documentation root that holds the configured spec and the content directory, and the spec path and the content directory path lie inside it. A document page points each relative link at the page that serves its target: the document page of a markdown file, the viewer page of the configured spec, or the viewer page at one of its operations when the link uses Stoplight's form. The portal resolves the target against the documentation root, as Stoplight does, and then against the markdown file's directory. A link leads nowhere when no page serves its target or when the target leaves the documentation root. Fragment links and absolute URLs stay as they are.

## Stages

Each stage ends with `devbox run make build lint test test-e2e`, the hole census (`grep -rn "HOLE(" --include='*.go' .`), the score card, the review page and a halt for review.

The plan gate changes the configuration inline: `-root-dir` and `-docs-path` in `main.parseConfig`, `main.openRoot` in place of `main.openSpecs` and `main.openDocs`, `portal.Config.Root` and `DocsPath`, the check of the content directory path in `portal.checkConfig`, and the content directory taken from the root in `portal.newRouter`, each with its tests. It declares the link functions as holes whose mock data leave every link as it is, as today, so every existing test stays green.

### Stage 1: links to documents

- Goal: a relative link opens the document page of its target, resolved against the documentation root and then against the file's directory, and a link whose target leaves the root or has no page leads nowhere.
- Requirement: 1, 3, 4, 5, 7, 8.
- Dependencies: none.
- Holes: `1 portal.site.renderMarkdown`, `1 portal.site.linkURL`.
- Acceptance: `TestDocLinks`.
- Size: 150 lines.

### Stage 2: links to the spec

- Goal: a link to the configured spec opens its viewer page, and a link to one of its operations in Stoplight's form opens the viewer page at that operation.
- Requirement: 2.
- Dependencies: 1.
- Holes: `2 portal.site.specURL`, `2 portal.operationRoute`.
- Acceptance: `TestSpecLinks`, and `TestOperationLinkOpensOperation` in the browser.
- Size: 130 lines.
