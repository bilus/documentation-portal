# View markdown files from a local directory

Issue: bilus/documentation-portal#13 (requirement R10, milestone MVP).
Ledger: `docs/plans/2026-09-29-local-markdown.ledger.md`.
Design: `docs/flow.dfd` and `docs/flow.3.dfd`, terms in `docs/vocabulary.md`, review page in `docs/review/index.html`.
Branch: `bilus/development`, which tracks `origin/master`. Base: `a34263b`.

## Requirements

Restated from the issue's acceptance criteria.

1. The operator names the content directory with `-docs-dir` or `DOCPORTAL_DOCS_DIR`. Without either, docportal serves the API spec as before, and no page links to the document list.
2. Startup stops with an error naming the content directory when the directory cannot be opened.
3. `GET /docs/` returns the document list: a link to the document page of every markdown file in the content directory, subdirectories included, sorted by path.
4. `GET /docs/{path}` returns the document page of the markdown file at that path, rendered as GitHub Flavored Markdown with at least headings, paragraphs, lists, links, emphasis and code blocks. The URL opens the same file on a direct load and on a reload.
5. On a document page, a relative link to another markdown file opens that file's document page, and an image with a relative URL shows through `/raw/{path}`.
6. The portal reads the content directory on every request, so a reload shows an edited, added or removed file without a restart.
7. Active content in a markdown file has no effect in the reader's browser: neither a `<script>` element nor a `javascript:` link runs.
8. `GET /raw/{path}` returns the image at that path, a PNG, JPEG, GIF, WebP or SVG file, with the image type of its extension, `X-Content-Type-Options: nosniff` and `Content-Security-Policy: sandbox`. Any other file gets a 404, so the route never serves `text/html`. A browser test shows that the `<script>` of an SVG does not run when a reader opens the SVG's raw URL.
9. A request for a missing file, for a file that is not a markdown file on `/docs/`, or for a path outside the content directory gets a 404 and no content of another file. A path outside means one with `..`, an absolute path, or a symlink out of the directory.
10. Every HTML page of the portal, the error pages included, shows the navigation bar: a link to the viewer page, and a link to the document list when a content directory is configured.
11. The tests read the sample documents: nested markdown files, a relative link, an image and a markdown file with a `<script>` element. The symlink out of the directory comes from the tests at run time.
12. The document list and every document page show the document sidebar: the markdown files that are not hidden, grouped by directory, drawn with the markup and the CSS classes of the Elements sidebar, so that its computed style matches the sidebar of the viewer page. It marks the open document the way Elements marks the open operation.

Out of scope: markdown from a source repository (#11), branches (#6), release notes (#12), a relative link to an API spec opening the viewer page, files other than images on `/raw/`, front matter, Mermaid, admonitions, raw HTML in markdown, and search.

## Questions and assumptions

- Q1. Names: the flag `-docs-dir` and the variable `DOCPORTAL_DOCS_DIR`, as the issue suggests, before #4 picks the names of its variables. Assumption: those names.
- Q2. Which files the list shows, an open question of #11. Assumption: every file named `*.md` or `*.markdown`, without hidden files and without the contents of hidden directories, whose names start with a dot. The list titles and sorts the files by path. A hidden markdown file also gets a 404 on `/docs/`.
- Q3. Markdown flavour. Assumption: CommonMark with goldmark's GFM extension (tables, strikethrough, autolinks, task lists) and automatic heading IDs, without front matter, Mermaid or admonitions.
- Q4. Raw HTML in markdown. Assumption: goldmark leaves it out, its default, and bluemonday's UGC policy sanitizes the rendered HTML as a second check.
- Q5. Link rewriting. A document page's URL mirrors its file's path, so a browser resolves a relative link between markdown files to the right document page with no rewriting. Assumption: rewrite image references to `/raw/` only, and leave link rewriting to #11, which adds `?branch=`.
- Q6. Navigation. A bar at the top of each page with two links. On the document list and the document pages, the document sidebar sits at the left, as the Elements sidebar does on the viewer page. Decided by the user, after the check of Elements 9.0.25 found no attribute or OpenAPI extension that adds entries to the Elements sidebar.
- Q7. Design. Request handling stays inside box 3.3, as decided for #2: the top diagram shows startup, and the handlers with their helpers belong to the box of `portal.newRouter`. Most of this feature runs per request, so a diagram of the request flow may help the reviewer. Assumption: as decided for #2.
- Q8. Metaphor. The user dropped the metaphor during #2. Assumption: this plan has none.
- Q10. Sidebar markup. The document sidebar copies the markup and the `sl-` classes of the Elements 9.0.25 sidebar, which are internal to Elements, so an upgrade of Elements can change them. `TestDocSidebarMatchesElements` compares the computed styles of both sidebars, so an upgrade that breaks the match fails the test. Assumption: the document sidebar's title is "Documents", and it leaves out Elements' "powered by Stoplight" link.
- Q9. Delivery, changed by the user after the plan's approval: one pull request with the whole issue. Stages are still reviewed at their boundaries, with handoffs in the chat. After the last approval, `bilus/development` keeps one commit per stage, and `devbox run -- gps rr` opens one pull request for the series. Commit messages stay under 15 words, which overrides the skill's two-sentence body.

## The change in brief

The operator can now name a content directory with `-docs-dir` or `DOCPORTAL_DOCS_DIR`. Startup opens it with `os.OpenRoot` after the specs directory, adds its handle to the portal configuration, and stops with an error naming the directory on a failure. With a content directory, the router sends `/docs/` to the document list, `/docs/{path}` to the document page of a markdown file and `/raw/{path}` to the raw file of an image. Without one, those paths get a 404, and no page links to them. The portal reads the content directory on every request, renders markdown as HTML without active content, and shows the error page for a path that names no markdown file, a hidden file or a file outside the content directory. Every HTML page shows the navigation bar.

## Stages

From stage 1 on, the document pages pass `s.nav()` and `s.sidebar(path)` to their template, and both return nothing until stages 3 and 4 fill them.

Each stage ends with `devbox run make build lint test test-e2e`, the hole census (`grep -rn "HOLE(" --include=*.go .`), the score card, the review page and a halt for review.

### Stage 1: the content directory and the document list

- Goal: docportal opens the content directory at startup, refusing one it cannot open, and `/docs/` lists its markdown files.
- Requirement: 1, 2, 3.
- Dependencies: none.
- Holes: `1 main.openDocs`, `1 portal.site.docList`.
- Acceptance: `TestStartupRejectsMissingDocsDir`, `TestOpenDocsKeepsReadsInside`, `TestDocList`.
- Size: 130 lines.

### Stage 2: the document page

- Goal: `/docs/{path}` shows a markdown file as HTML without active content, with its images under `/raw/`, and every other path under `/docs/` gets a 404.
- Requirement: 4, 5, 6, 7, 9.
- Dependencies: 1.
- Holes: `2 portal.site.docPage`, and the helpers its fill declares.
- Acceptance: `TestDocPage`, `TestDocPageDropsActiveContent`, `TestDocPageReadsEachRequest`, `TestDocsNotFound`, `TestStartupKeepsDocsInside`.
- Size: 220 lines.

### Stage 3: raw files and the navigation

- Goal: `/raw/{path}` serves the images of the content directory with headers that stop the browser from running them, every page shows the navigation bar, and README.md describes `-docs-dir` and the new routes.
- Requirement: 8, 10.
- Dependencies: 2.
- Holes: `3 portal.site.rawFile`, `3 portal.site.nav`.
- Acceptance: `TestRawFile`, `TestRawSVGRunsNoScript`, `TestPagesShareNavigation`.
- Size: 190 lines.

### Stage 4: the document sidebar

- Goal: the document list and the document pages show the document sidebar, in the style of the sidebar of the viewer page.
- Requirement: 12.
- Dependencies: 2.
- Holes: `4 portal.site.sidebar`.
- Acceptance: `TestDocSidebar`, `TestDocSidebarMatchesElements`.
- Size: 150 lines.
