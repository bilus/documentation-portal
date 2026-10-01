# documentation-portal

`docportal` serves the documentation of OpenAPI 3.0 and 3.1 specs, rendered
with [Stoplight Elements](https://github.com/stoplightio/elements), and of
markdown files, each as a section of its navigation bar.

Everything runs through [devbox](https://www.jetify.com/devbox). The first
`make` target that needs them downloads the Elements assets, checked against a
pinned SHA-256, into `portal/elements/`, where the binary embeds them.

    devbox run make run ARGS='-config testdata/environment.yaml'

Then open http://localhost:8080. The configuration file lists the sections of
the portal, each under its title in the navigation bar: a spec, or a content
directory of markdown files with an optional toc file.

    sections:
      - title: API
        type: spec
        input: specs/petstore-3.1.yaml
      - title: Guides
        type: docs
        input: docs
        toc: toc.json

The directory of the configuration file is the documentation root, and every
input and toc path is relative to it. Each flag falls back to an environment
variable: `-addr` to `DOCPORTAL_ADDR` (default `:8080`), `-config` to
`DOCPORTAL_CONFIG` (default `environment.yaml`) and `-hide-try-it` to
`DOCPORTAL_HIDE_TRY_IT` (default `false`). A program that mounts the portal
passes the sections in `portal.Config`, or reads them with `portal.ReadConfig`.

The URLs of a section carry its slug: its title in lower case, with each run
of characters other than letters and digits as one dash, such as `store-api`
for Store API. A spec section's viewer page is at `/specs/{slug}`, and its raw
spec at `/api/specs/{slug}`; `/` opens the first section's page. docportal
does not start without sections, with a section that has no title, input or
known type, with two sections of one slug, with a path outside the
documentation root, or with a toc on a spec section.

The viewer page shows the Try It console of Stoplight Elements, which sends
requests from the reader's browser to the servers of the spec, so those
servers must allow the portal's origin through CORS. `-hide-try-it` hides the
console.

A docs section's document list at `/docs/{slug}/` lists the markdown files
of its content directory, `/docs/{slug}/{path}` renders one as HTML without
scripts, and `/raw/{slug}/{path}` serves its PNG, JPEG, GIF, WebP and SVG
images. A relative link in a markdown file resolves against the
documentation root, as on Stoplight, and then against the file's own
directory, and it shows as plain text when no page serves its target. A link
to a markdown file opens its document page in the first docs section whose
content directory holds it, so content directories may nest. A link to a
spec opens its section's viewer page, and a link to one of its operations in
Stoplight's form, such as `openapi.yaml/paths/~1pets/get`, opens the viewer at
that operation. Of two spec sections with one spec, links open the first. The
portal reads nothing outside the documentation root, follows no symlink in a
content directory, and under `/specs/` and `/api/specs/` serves no file of
the root except the specs of the spec sections.

A docs section's `toc` names a Stoplight `toc.json` inside the documentation
root, such as `toc.json`. The section's document sidebar then shows its
entries in its order and under its titles: an entry for a markdown file links
its document page, in any docs section, one for a spec or one of its
operations links its section's viewer page, and an http or https URL stays as
it is. The sidebar leaves out an entry that no page serves. A missing or
invalid toc file brings back the list of markdown files, with a line in the
log.

## Chat

With `-chat-model` or `DOCPORTAL_CHAT_MODEL` naming an Anthropic model, such as
`claude-opus-5-5`, the portal serves a chat page at `/chat`, where readers ask
questions about the API. The model answers from the published specs and the
markdown files of every section alone, through read-only tools, and links the
pages it used. The page's heading names the API of the first spec section.
Only the portal's own pages become links in an answer; any other URL shows as
text. The Anthropic SDK reads its credentials from `ANTHROPIC_API_KEY`.

    ANTHROPIC_API_KEY=... devbox run make run ARGS='-config testdata/environment.yaml -chat-model claude-opus-5-5'

Each client may ask 20 questions an hour, a conversation holds 20 questions
and 512 KiB of messages and lookups, and one answer may make 12 lookups. A
client is an IPv4 address or an IPv6 /64 network. Behind a proxy, every reader
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
