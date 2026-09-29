# Render API spec bundles with Stoplight Elements

Issue: bilus/documentation-portal#2 (requirement R1, milestone MVP).
Ledger: `docs/plans/2026-09-29-openapi-elements-viewer.ledger.md`.
Design: `docs/flow.dfd`, terms in `docs/vocabulary.md`, review page in `docs/review/index.html`.
Branch: `claude/awesome-curie-he65gq`.

## Requirements

Restated from the issue's acceptance criteria. Requirement 9 is added so the MVP runs before #4 exists.

1. Given a valid API spec, `GET /specs/{spec path}` returns the viewer page, which renders the spec with Stoplight Elements and shows its `info.title`, `info.version` and `info.description`.
2. Every operation of the spec appears in the Elements navigation. Selecting one shows its method, path, parameters, request body and responses, including their schemas.
3. Models under `components/schemas` are listed and open on their own.
4. Internal `$ref`s are resolved, and no raw `$ref` text appears on the page.
5. Requirements 1 to 4 hold for both sample bundles, OpenAPI 3.0 and OpenAPI 3.1.
6. An invalid spec gets an error page naming the file and the reason (422), and a missing spec a 404 naming the file. The process keeps serving other requests.
7. `GET /api/specs/{spec path}` returns the raw spec as `application/yaml`. No request returns any other file of the specs directory.
8. A headless-browser test over the sample bundles checks the rendered operation list and a schema resolved from a `$ref`.
9. docportal takes the address, the directory name and the spec path from flags or the environment, and fails at startup, naming the cause, when the specs directory cannot be opened, the spec path leaves it, or the Elements assets are missing.

Out of scope: several specs (#3), Git sources (#4), branches (#6), markdown (#13), authentication (#8, #9), Swagger 2.0, external `$ref`s and CI.

## Questions and assumptions

Approved at the first gate on 2026-09-29, before the switch to this version of the method:

- D1: the portal reads specs from an `io/fs.FS`, a local directory opened with `os.OpenRoot`, not from #4's `Source`. #4 can later hand it an `fs.FS` view of a commit.
- D2: `devbox run make setup` downloads `@stoplight/elements` 9.0.25, checks the tarball's SHA-256 and extracts the Elements assets (`web-components.min.js`, `styles.min.css` and the Apache-2.0 `LICENSE`), which git ignores and the binary embeds. Their directory follows the code that embeds them, which stage 2 draws.
- D3: an API spec must parse as YAML and have an `openapi` field of 3.0.x or 3.1.x and a non-empty `info.title`. Swagger 2.0 is rejected with a message saying so.
- D4: Elements' Try It console is hidden.
- D5: Elements uses hash routing, so links to operations need no server routes, and `/` redirects to the viewer page.
- D6: the browser test uses chromedp behind the `e2e` build tag, with `CHROME_BIN`, which the Makefile sets in cloud sessions, and `--no-sandbox` only as root.
- D7: `portal.New(portal.Config) (http.Handler, error)` is the only public API, pre-1.0.
- D8: handoffs in chat, commits on the branch above, no pull request per stage.

New at this gate:

- Open question: the top function is `main.startup`, so the top diagram shows startup, and the portal's request-time I/O (reads through the directory handle, pages sent to the reader) is drawn at no level. The HTTP server calls the portal after startup, so no box's function performs that I/O, and request handling would stay a helper of the routing step that stage 2 draws. The design review recommends drawing it. The way to do that is a top function `run` whose fourth box, "Serve the portal", calls `http.ListenAndServe` with the reads from `<Specs directory>` and the pages to the reader's browser beside it. The cost is a smoke test against a real listener on a fixed port, and a `serve` hole whose mock data is the real body. Assumption for this gate: startup only, as drawn.
- The address travels on the arrows from step 1 past steps 2 and 3 to the HTTP server, the way dfdreview's own design carries `options`, `design` and `code index` past `draw.Views`. The score cannot see it; the handoff lists it. The design review asked for a store instead, since `portal.New` neither takes nor returns the address; decided against, to follow the method's own example.
- The flag for the spec path is `-spec-path`, to match `DOCPORTAL_SPEC_PATH`.
- The skeleton declares the top diagram's functions only. The checks, the Elements assets and the routing come with `docs/flow.3.dfd` in stage 2, so there are no packages under `internal/` yet.
- The acceptance tests go through `main.startup`, `main.parseConfig` and `portal.New`. The refusal of missing Elements assets cannot be driven from there, because the assets are embedded at build time, so stage 3 tests it at the step that checks them.
- Requirement 7 used to say that any other path, `..` included, gets a 404. `net/http` answers a path with `..` with a redirect to the cleaned path, so the requirement now states the property that matters: no other file is served.
- The skeleton's `parseConfig` mock returns the smoke test's configuration, which points at `testdata/specs`. Stage 1 replaces it.

## The change in brief

docportal gains its startup and a portal for one API spec. Startup reads the configuration from the flags and the environment, opens the specs directory so that the portal cannot read outside it, and builds the portal, refusing a spec path that leaves the specs directory or missing Elements assets. The HTTP server then serves the portal at the address. The viewer page embeds Stoplight Elements, which renders the configured spec in the reader's browser from the raw spec, and an invalid or missing spec gets an error page naming the file.

## Stages

Each stage ends with `devbox run make build lint test test-e2e`, the hole census (`grep -rn "HOLE(" --include=*.go --include=Makefile .`), a design review when a diagram changed, the score card, the review page and a halt for review.

### Stage 1: configuration and the specs directory

- Goal: docportal reads its configuration from the flags and the environment, and opens the specs directory so that no read leaves it, refusing a directory it cannot open.
- Requirement: 9.
- Dependencies: none.
- Holes: `1 main.parseConfig`, `1 main.openSpecs`.
- Acceptance: `TestParseConfig`, `TestStartupRejectsMissingSpecsDir`.
- Size: 60 lines.

### Stage 2: the steps of building the portal

- Goal: the new level. `docs/flow.3.dfd` draws the steps of `portal.New`, expected to be checking the portal configuration, loading the Elements assets and routing the requests. `portal.New` calls them in order, each declared with a hole of stage 3 or 4.
- Requirement: 9, 1, 7.
- Dependencies: 1.
- Holes: `2 portal.New`.
- Acceptance: `TestNewServesSpec`, the smoke test of process 3, written in this stage and passing on the new holes' mock data.
- Size: 100 lines.

### Stage 3: checks and the Elements assets

- Goal: the portal refuses a spec path outside the specs directory and missing Elements assets, and `make setup` fetches the pinned assets and checks them.
- Requirement: 9, 1.
- Dependencies: 2.
- Holes: `3 portal.checkConfig`, `3 portal.loadAssets`, and `3 portal.assetsIn`, the helper its fill declared, with the Makefile's `setup` target.
- Acceptance: `TestStartupRejectsSpecPathOutsideDir`, and the asset check's own test, written with the fill.
- Size: 90 lines.

### Stage 4: the viewer page, the raw spec and the Elements assets on the wire

- Goal: a reader opens the viewer page and Stoplight Elements renders the spec, the raw spec and the Elements assets are served, and an invalid or missing spec gets an error naming the file.
- Requirement: 1 to 8.
- Dependencies: 3.
- Holes: `4 portal.newRouter`, and the helpers its fill declares: the handlers, loading and checking a spec, and the page templates.
- Acceptance: `TestIndexRedirects`, `TestViewerPage`, `TestRawSpec`, `TestSpecErrorPages`, `TestElementsAssets`, `TestViewerRendersSampleBundles`.
- Size: 280 lines, with a README section on setup and running.
