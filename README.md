# documentation-portal

`docportal` serves the documentation of OpenAPI 3.0 and 3.1 specs, rendered
with [Stoplight Elements](https://github.com/stoplightio/elements), and of
markdown files, each as a section of its navigation bar.

Everything runs through [devbox](https://www.jetify.com/devbox). The first
`make` target that needs them downloads the Elements assets, checked against a
pinned SHA-256, into `portal/elements/`, where the binary embeds them.

    devbox run make run ARGS='-config testdata/environment.yaml'

Then open http://localhost:8080. The configuration file lists the portals,
each a named set of sections, and each section shows under its title in the
navigation bar: a spec, or a content directory of markdown files with an
optional toc file.

    portals:
      - name: Petstore
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
handler passes the portals in `portal.Config`, or reads them with
`portal.ReadConfig`.

## A bucket folder as the documentation root

`-root` (`DOCPORTAL_ROOT`) names a folder of a bucket as the documentation
root instead of a local directory, by its [Go CDK](https://gocloud.dev/howto/blob/)
URL: `gs://docs-bucket?prefix=portal/` on GCS, `s3://docs-bucket?region=eu-west-1&prefix=portal/`
on S3, or `file:///srv/docs?prefix=portal/` on the local file system, where
`prefix` selects the folder and ends in a slash. `-config` then names the
configuration file by its path inside the folder. docportal needs read access
alone, with credentials from the platform's usual sources: Application Default
Credentials on GCP, and the SDK's default chain on AWS.

    docportal -root 'gs://docs-bucket?prefix=portal/' -config environment.yaml

docportal reads the whole folder into memory at startup, as one snapshot, and
refuses a folder over `-max-size` (`DOCPORTAL_MAX_SIZE`) MiB, 256 by default
and 0 for no limit. Every `-refresh` (`DOCPORTAL_REFRESH`, a duration, one
minute by default, 0 for never) it lists the folder again, and a change that
two checks in a row report alike becomes a new snapshot, whose pages replace
the old ones in one step, so that every request reads one version of the
documentation and links between documents lead to one version. A check or a
reload that fails, from an unreachable bucket or a configuration file that
docportal refuses, keeps the old snapshot and writes the cause to the log, and
the next check tries again. The chat keeps its conversations and its question
counts across snapshots. A local directory is read live, as before.

The `file://` form serves local tests of the bucket flow. Unlike the local
directory of `-config` alone, which follows no symlink out of the root, it
reads through a symlink to a file outside the folder, as Go CDK's file driver
does, so keep it to folders that nobody else can write to.

A program that uses the library opens the folder with `source.OpenBucket`,
after a blank import of the Go CDK drivers it needs, such as
`gocloud.dev/blob/gcsblob`; loads the first snapshot with `source.Load`;
builds the portal handler from the snapshot's root; and wraps the handler in
`source.NewReloader`, which rebuilds it with the program's own builder for
each settled change.

Portals and sections have slugs: the name or the title in lower case, with
each run of characters other than letters and digits as one dash, such as
`store-api` for Store API. Every page of a portal is under
`/portals/{portal}/`, where `{portal}` is the portal's slug: a spec section's
viewer page is at `/portals/{portal}/specs/{slug}` and its raw spec at
`/portals/{portal}/api/specs/{slug}`, and `/portals/{portal}/` opens the
portal's first section. With one portal, `/` opens it; with several, `/` lists
them, and a menu on the right of the navigation bar, labelled with the page's
portal, opens any other. Links and toc entries stay within their portal. docportal does not start
without portals, with a portal that has no name, with two portals of one slug,
or with a problem in a portal's sections: a section that has no title, input
or known type, two sections of one slug in a portal, a path outside the
documentation root, or a toc on a spec section.

The viewer page shows the Try It console of Stoplight Elements, which sends
requests from the reader's browser to the servers of the spec, so those
servers must allow the portal's origin through CORS. `-hide-try-it` hides the
console.

A docs section's document list at `/portals/{portal}/docs/{slug}/` lists the
markdown files of its content directory, `/portals/{portal}/docs/{slug}/{path}`
renders one as HTML without scripts, and `/portals/{portal}/raw/{slug}/{path}`
serves its PNG, JPEG, GIF, WebP and SVG images. A relative link in a markdown file resolves against the
documentation root, as on Stoplight, and then against the file's own
directory, and it shows as plain text when no page serves its target. A link
to a markdown file opens its document page in the section of the page that
links it, when that section holds it, and else in the first docs section that
holds it, so content directories may nest. A link to a
spec opens its section's viewer page, and a link to one of its operations in
Stoplight's form, such as `openapi.yaml/paths/~1pets/get`, opens the viewer at
that operation. Of two spec sections with one spec, links open the first. The
portal reads nothing outside the documentation root, follows no symlink in a
content directory, and under a portal's `/specs/` and `/api/specs/` serves no
file of the root except the specs of its spec sections.

A docs section's `toc` names a Stoplight `toc.json` inside the documentation
root, such as `toc.json`. The section's document sidebar then shows its
entries in its order and under its titles: an entry for a markdown file links
its document page, in any docs section, one for a spec or one of its
operations links its section's viewer page, and an http or https URL stays as
it is. The sidebar leaves out an entry that no page serves. The section's
document list shows the same entries, with a heading for each group and
divider, and then the markdown files without an entry, under Other documents.
A missing or invalid toc file brings back the list of markdown files, with a
line in the log.

## Previews

With `-previews` (`DOCPORTAL_PREVIEWS`) naming a previews location, a folder
of the bucket folder such as `previews/`, docportal serves previews of
documentation before its publication, such as a pull request's. A CI job
uploads the documentation of each pull request to a preview folder of its own
under the location, such as `previews/pr-123/`, with a configuration file at
the path of `-config` and the files of its sections, and posts a link to
`/previews/pr-123` on the pull request. The published documentation leaves
the previews location out, and its size limit counts none of it.

    docportal -root 'gs://docs-bucket?prefix=portal/' -previews previews/

The link switches the reader's browser to the preview and opens `/`, and
`/previews/pr-123/{path}` opens `/{path}` instead, so that a link can lead to
a changed page. From then on, every page, raw file and raw spec comes from the
preview folder, and a banner above the navigation bar names the folder, with
a link to `/previews/`, the way back to the published documentation. A
session cookie, `portal-preview`, holds the folder, so a browser shows one
preview at a time, and every response in a preview carries
`Cache-Control: private`. A preview has no chat. A folder name is one path
segment of ASCII letters, digits, `.`, `_` and `-`, other than `.` and `..`;
any other name, and a folder without a configuration file, gets a 404 page,
with the reader still in the preview or the published documentation of the
request.

docportal loads a preview folder at its first request, as a snapshot of its
own, and checks it every `-refresh`, like the bucket folder. A snapshot leaves
memory after `-preview-idle` (`DOCPORTAL_PREVIEW_IDLE`, one hour by default, 0
for never) without a request, and at most `-max-previews`
(`DOCPORTAL_MAX_PREVIEWS`, 10 by default, 0 for no limit) stay in memory, the
least recently used leaving first. Each holds up to `-max-size`, so
docportal's snapshots take up to eleven times that size with the defaults. A
check that finds a preview folder empty, as after the CI job deletes it, ends
the preview, and its readers return to the published documentation with a
notice.

Any site can link `/previews/pr-123` and switch a reader to a preview. The
banner makes the switch visible, so keep the previews location to the CI
job's uploads.

A program that embeds the portal leaves the previews location out of the
published bucket folder with `source.Bucket.Without`, keeps the snapshots of
the preview folders with `source.NewPreviews`, over the folders of
`source.Bucket.Folder`, and wraps the published portal handler in
`portal.WithPreviews`:

    published, err := bucket.Without("previews") // bucket from source.OpenBucket
    ...
    location, err := bucket.Folder("previews")
    ...
    previews := source.NewPreviews(ctx, func(name string) (source.Source, error) {
    	return location.Folder(name)
    }, build, time.Minute, time.Hour, 10)
    h := portal.WithPreviews(reloader, portal.PreviewsConfig{Open: previews.Handler, Access: access})

Here `reloader` serves the snapshots of `published`, as above, and `build`
builds a preview folder's portal handler, with the program's hooks and
without the chat. With an access hook, the program decides who may open
previews: the hook's access opens a preview only through the method
`Preview(folder string) bool` of `portal.PreviewAccess`, so a hook without it
opens none. `portal.Everything` opens every preview, and without a hook every
reader may open any preview. The previews handler asks the access hook once
for each request with a folder to switch to or a preview to serve.

## Chat

With `-chat-model` or `DOCPORTAL_CHAT_MODEL` naming an Anthropic model, such as
`claude-opus-5-5`, every portal has a chat page at `/portals/{portal}/chat`,
where readers ask questions about its APIs. The model answers from the
published specs and the markdown files of the portal's sections alone, through
read-only tools, and links the pages it used. The page's heading and the
model's instructions name the API of every spec section of the portal. Only
the portal handler's own pages become links in an answer; any other URL shows
as text. The Anthropic SDK reads its credentials from `ANTHROPIC_API_KEY`.

    ANTHROPIC_API_KEY=... devbox run make run ARGS='-config testdata/environment.yaml -chat-model claude-opus-5-5'

Each client may ask 20 questions an hour, in all portals together, a
conversation holds 20 questions
and 512 KiB of messages and lookups, and one answer may make 12 lookups. A
client is an IPv4 address or an IPv6 /64 network. Behind a proxy, every reader
shares the proxy's address, unless the program that embeds the chat names its
readers (see below). The page uses live-templ, a private module that Go
fetches with git, so devbox sets `GOPRIVATE` for it. After changing
`chat/page.templ`, run `devbox run make generate`.

## Embedding the portal

The packages `portal`, `chat` and `source` make up the embedding API, which
is not stable yet. A program builds the portal handler with `portal.New` from a
`portal.Config`, whose portals `portal.ReadConfig` can read from a
configuration file, wraps the handler in its own middleware and mounts it in
its own server, beside routes of its own. docportal is such a program.

    cfg, err := portal.ReadConfig(os.DirFS("docs"), "environment.yaml")
    if err != nil {
    	log.Fatal(err)
    }
    cfg.Access, cfg.Account = access, account // request hooks, below
    h, err := portal.New(cfg)
    if err != nil {
    	log.Fatal(err)
    }
    mux := http.NewServeMux()
    mux.Handle("/auth/", authRoutes) // the program's own routes
    mux.Handle("/", signIn(h))       // its sign-in middleware around the portal
    log.Fatal(http.ListenAndServe(":8080", mux))

The portal handler serves `/`, `/portals/`, `/previews/`, `/assets/elements/`
and, with a chat, the chat's socket at `/live/websocket` and its scripts. A pattern of
the program's own, such as `/auth/`, is more specific than `/`, so the parent
mux sends its requests to the program. The portal knows no identity
provider: the program's middleware signs each reader in and keeps the
reader's identity in the request's context. The core packages, `portal`,
`source`, `chat` and `anthropicmodel`, import no OAuth or OpenID Connect
library and no identity provider's package, which `make lint` checks;
docportal's GCS and S3 drivers use such packages for the bucket's own
credentials alone. The chat's socket opens with a request that passes
through the same middleware.

Request hooks, optional functions of the request, read that identity:

- `portal.Config.Access` returns the reader's access to the portals and
  sections (see Access per reader), and, through `portal.PreviewAccess`, to
  the previews (see Previews).
- `portal.Config.Account` returns the reader's account links, such as the
  reader's name and a sign-out link, which end the navigation bar of every
  page and of the chat page. A link without a URL shows its text alone,
  and the home page shows the links in a bar of their own, so that a reader
  who sees no portal can still sign out. Relative URLs and http, https and
  mailto URLs work on every page.
- `chat.Config.Reader` returns the reader's ID for the chat's question limit
  (see The chat's question limit per reader).

An account hook that names a signed-in reader and links the program's own
routes:

    cfg.Account = func(r *http.Request) []portal.AccountLink {
    	name, ok := r.Context().Value(nameKey{}).(string)
    	if !ok {
    		return []portal.AccountLink{{Label: "Sign in", URL: "/auth/sign-in"}}
    	}
    	return []portal.AccountLink{{Label: name}, {Label: "Sign out", URL: "/auth/sign-out"}}
    }

The portal handler calls each of its hooks once for each request. With an
access hook or an account hook, every response carries
`Cache-Control: private`, so that no shared cache gives one reader's page to
another. A chat page keeps the access, the account links and the reader ID of
its GET until it loads again. The example of `portal.New` in the package
documentation runs such a program.

## Access per reader

A program that embeds the portal handler can open portals and sections to
some readers alone. `portal.Config.Access` takes an access hook, a function
of the request that returns the reader's `portal.Access`. The portal handler
calls the hook once for each request, and asks the access about each portal
and section of the configuration, as the configuration gives them, labels
included: `Portal(p)` and `Section(p, s)`. A section is visible when the
access allows it and its portal, and a portal when the access allows it and
at least one of its sections.

A portal or section that is not visible answers as a missing one. The home
page, the portal menu and the navigation bar leave it out, each of its routes
answers with the 404 page of a missing portal or section, a markdown link to
one of its pages shows as plain text, the document sidebar leaves out its toc
entries, and the chat answers without it. With one visible portal, `/` opens
it, and with none, the home page says so. Without a hook, every reader sees
everything; docportal sets none.

The portal knows no identity provider. The program authenticates each
request in its own middleware, keeps the reader's claims in the request's
context, and maps them to portals and sections in the hook. Labels in the
configuration file give the rules names that survive a rename, since a
section's slug follows its title:

    portals:
      - name: Delivery
        labels: [partner]
        sections:
          - title: API
            type: spec
            input: specs/delivery.yaml
          - title: Internal notes
            type: docs
            input: internal
            labels: [staff]

A middleware puts the reader's groups into the request's context, and the
hook turns them into an access, here one that opens a portal or a section
without labels to every reader, and one with labels to the members of one of
them:

    type groupsKey struct{}

    func withGroups(next http.Handler) http.Handler {
    	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    		groups := groupsOf(r) // the program's own sign-in, such as a claim of a verified token
    		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), groupsKey{}, groups)))
    	})
    }

    type groups []string

    func (g groups) Portal(p portal.Portal) bool                    { return g.open(p.Labels) }
    func (g groups) Section(_ portal.Portal, s portal.Section) bool { return g.open(s.Labels) }

    func (g groups) open(labels []string) bool {
    	return len(labels) == 0 || slices.ContainsFunc(labels, func(l string) bool { return slices.Contains(g, l) })
    }

    func main() {
    	cfg, err := portal.ReadConfig(os.DirFS("docs"), "environment.yaml")
    	if err != nil {
    		log.Fatal(err)
    	}
    	cfg.Access = func(r *http.Request) (portal.Access, error) {
    		g, _ := r.Context().Value(groupsKey{}).([]string)
    		return groups(g), nil
    	}
    	h, err := portal.New(cfg)
    	if err != nil {
    		log.Fatal(err)
    	}
    	log.Fatal(http.ListenAndServe(":8080", withGroups(h)))
    }

With these rules, a partner reads the API, a partner on the staff reads the
internal notes too, and a reader without groups gets the home page with no
portal.

An error from the hook hides everything from the reader and goes to the log,
so the hook returns an access that hides everything for a reader without
rights, and an error only for a failure, such as a permission service out of
reach. With a hook, every response carries `Cache-Control: private`, so that
no shared cache gives one reader's page to another. `portal.Everything` is
the access of a reader who may see everything.

A reader of such a program opens a preview only with an access that also
has the method `Preview`, such as one that opens every preview to the staff:

    func (g groups) Preview(string) bool { return slices.Contains(g, "staff") }

A docs section serves its whole content directory, nested directories
included, so keep the directory of a section for some readers out of the
content directory of every section for others.

The chat follows the hook when its routes, from `chat.Chat.Routes`, are the
configuration's `Chat`: a chat page answers from the sections of its portal
visible to the reader, and its heading and the model's instructions name only
their APIs. The page keeps the reader's access from its load, signed into its
session, so a change of the reader's access reaches an open chat page when it
loads again. A later snapshot that changes a portal's configuration closes that
portal to an open page until the page loads again; without a hook, an open
page follows each snapshot. `portal.AccessOf(r)` gives the access that the portal handler
found for a request, and an access that allows nothing to a request served by
no portal handler; `Library.For(access)` limits a library to a reader, and
`chat.Chat.Ask` takes the reader's access. A conversation keeps a separate
history for each set of visible sections, so that no answer reads an earlier
lookup from a section hidden from its reader.

## The chat's question limit per reader

A program that signs its readers in can count each reader's questions against
one limit, from every address the reader uses. `chat.Config.Reader` takes a
reader hook, a function of the request that returns the reader's ID, such as
the subject of the reader's verified token, or an empty string for a reader
who is not signed in:

    c, err := chat.New(chat.Config{Model: m, Libraries: libs,
    	Reader: func(r *http.Request) string {
    		id, _ := r.Context().Value(subjectKey{}).(string) // from the program's middleware
    		return id
    	}})

The chat asks the hook when a chat page loads, through the portal handler,
and keeps the ID in the page's session for the page's socket. live-templ signs
the session, so a reader cannot change the ID, but does not encrypt it, so the
reader's browser can read it: return an ID that the reader may see. A loaded
page asks as its reader for up to 14 days, the life of live-templ's session,
even after a sign-out, and so does any copy of the page. With the
hook, a chat page answers with `Cache-Control: private`, so that no shared
cache gives one reader's page to another. A reader without an ID, and every
reader without the hook, counts by client, and `chat.Limits` sets the limit
and its window for readers and clients alike. A program that calls
`chat.Chat.Ask` itself asks as `chat.AskerOf(id, client)`, which counts the
question against the limit of the reader's chat pages, or against the
client's for an empty ID.

## Tests

    devbox run make lint       # go vet, gofmt and the core packages' imports
    devbox run make test       # unit and acceptance tests
    devbox run make test-e2e   # renders the sample specs in headless Chrome

The browser test needs Chrome or Chromium. Set `CHROME_BIN` if chromedp does
not find it.

The design lives in `docs/`: the data flow diagrams (`flow.dfd` and its
child diagrams `flow.3.dfd`, `flow.3.4.dfd`, `flow.9.dfd` and `flow.11.dfd`), the
vocabulary, and the plans and ledgers of the changes.
