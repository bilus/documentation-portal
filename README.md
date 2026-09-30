# documentation-portal

`docportal` serves the documentation of one OpenAPI 3.0 or 3.1 spec, rendered
with [Stoplight Elements](https://github.com/stoplightio/elements), and the
markdown files of an optional content directory.

Everything runs through [devbox](https://www.jetify.com/devbox). The first
`make` target that needs them downloads the Elements assets, checked against a
pinned SHA-256, into `portal/elements/`, where the binary embeds them.

    devbox run make run ARGS='-root-dir testdata -spec-path specs/petstore-3.1.yaml -docs-path docs'

Then open http://localhost:8080. The documentation root holds the spec and the
content directory, and `-spec-path` and `-docs-path` are relative to it. Each
flag falls back to an environment variable: `-addr` to `DOCPORTAL_ADDR`
(default `:8080`), `-root-dir` to `DOCPORTAL_ROOT_DIR` (default `.`),
`-spec-path` to `DOCPORTAL_SPEC_PATH` (default `openapi.yaml`), `-docs-path` to
`DOCPORTAL_DOCS_PATH` (default none) and `-hide-try-it` to
`DOCPORTAL_HIDE_TRY_IT` (default `false`).

The viewer page shows the Try It console of Stoplight Elements, which sends
requests from the reader's browser to the servers of the spec, so those
servers must allow the portal's origin through CORS. `-hide-try-it` hides the
console.

With a content directory, `/docs/` lists its markdown files, `/docs/{path}`
renders one as HTML without scripts, and `/raw/{path}` serves its PNG, JPEG,
GIF, WebP and SVG images. The portal reads nothing outside the documentation
root, and under `/specs/` and `/api/specs/` it serves no file of the root
except the spec.

## Tests

    devbox run make test       # unit and acceptance tests
    devbox run make test-e2e   # renders the sample specs in headless Chrome

The browser test needs Chrome or Chromium. Set `CHROME_BIN` if chromedp does
not find it.

The design lives in `docs/`: the data flow diagrams (`flow.dfd`,
`flow.3.dfd`), the vocabulary, and the plan and ledger of this change.
