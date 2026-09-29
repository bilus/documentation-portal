# documentation-portal

`docportal` serves the documentation of one OpenAPI 3.0 or 3.1 spec, rendered
with [Stoplight Elements](https://github.com/stoplightio/elements).

Everything runs through [devbox](https://www.jetify.com/devbox). The first
`make` target that needs them downloads the Elements assets, checked against a
pinned SHA-256, into `portal/elements/`, where the binary embeds them.

    devbox run make run ARGS='-specs-dir testdata/specs -spec-path petstore-3.1.yaml'

Then open http://localhost:8080. Each flag falls back to an environment
variable: `-addr` to `DOCPORTAL_ADDR` (default `:8080`), `-specs-dir` to
`DOCPORTAL_SPECS_DIR` (default `.`) and `-spec-path` to `DOCPORTAL_SPEC_PATH`
(default `openapi.yaml`). The portal reads nothing outside the specs directory
and serves no file of it except the spec.

## Tests

    devbox run make test       # unit and acceptance tests
    devbox run make test-e2e   # renders the sample specs in headless Chrome

The browser test needs Chrome or Chromium. Set `CHROME_BIN` if chromedp does
not find it.

The design lives in `docs/`: the data flow diagrams (`flow.dfd`,
`flow.3.dfd`), the vocabulary, and the plan and ledger of this change.
