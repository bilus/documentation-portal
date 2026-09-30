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
GIF, WebP and SVG images. A relative link in a markdown file resolves against
the documentation root, as on Stoplight, and then against the file's own
directory, and it shows as plain text when no page serves its target. A link
to the spec opens the viewer page, and a link to one of its operations in
Stoplight's form, such as `openapi.yaml/paths/~1pets/get`, opens the viewer at
that operation. The portal reads nothing outside the documentation root,
follows no symlink in the content directory, and under `/specs/` and
`/api/specs/` serves no file of the root except the spec.

## Chat

With `-chat-model` or `DOCPORTAL_CHAT_MODEL` naming an Anthropic model, such as
`claude-opus-5-5`, the portal serves a chat page at `/chat`, where readers ask
questions about the API. The model answers from the published spec and the
markdown files alone, through read-only tools, and links the pages it used.
The Anthropic SDK reads its credentials from `ANTHROPIC_API_KEY`.

    ANTHROPIC_API_KEY=... devbox run make run ARGS='-root-dir testdata -spec-path specs/petstore-3.1.yaml -docs-path docs -chat-model claude-opus-5-5'

Each client address may ask 20 questions an hour, a conversation holds 20
questions, and one answer may make 12 lookups. Behind a proxy, every reader
shares the proxy's address. The page uses live-templ, a private module that Go
fetches with git, so devbox sets `GOPRIVATE` for it. After changing
`chat/page.templ`, run `devbox run make generate`.

## Tests

    devbox run make test       # unit and acceptance tests
    devbox run make test-e2e   # renders the sample specs in headless Chrome

The browser test needs Chrome or Chromium. Set `CHROME_BIN` if chromedp does
not find it.

The design lives in `docs/`: the data flow diagrams (`flow.dfd`,
`flow.3.dfd`), the vocabulary, and the plan and ledger of this change.
