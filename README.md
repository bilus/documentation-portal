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
shares the proxy's address. The page uses live-templ, a private module that Go
fetches with git, so devbox sets `GOPRIVATE` for it. After changing
`chat/page.templ`, run `devbox run make generate`.

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
`chat.Chat.Ask` takes the reader's access.

## Tests

    devbox run make test       # unit and acceptance tests
    devbox run make test-e2e   # renders the sample specs in headless Chrome

The browser test needs Chrome or Chromium. Set `CHROME_BIN` if chromedp does
not find it.

The design lives in `docs/`: the data flow diagrams (`flow.dfd` and its
child diagrams `flow.3.dfd`, `flow.3.4.dfd` and `flow.9.dfd`), the
vocabulary, and the plans and ledgers of the changes.
