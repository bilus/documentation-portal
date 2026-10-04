# portalapp: an example application on the portal library

portalapp embeds the portal library of this repository in a program of its
own. It serves the documentation of a bucket folder, refreshed as the folder
changes, with previews of pull requests; it signs readers in through Auth0
or GitHub; and it opens portals, sections and previews to each reader by an
access file, which maps the reader's claims to the labels of the
configuration file. With an Anthropic API key, it runs the chat, with one
question limit for each reader. It uses the library's documented API
alone: the packages `portal`, `source`, `signin`, `chat` and
`anthropicmodel` (see Embedding the portal in the repository's README).

The example is a Go module of its own, so that its dependencies stay out of
the library's `go.mod`; a `replace` directive in its `go.mod` points at the
library in this checkout. Copy any part of it into a program of your own.

## A local run against the mocks

The package `mocks` holds stand-ins for Auth0 and GitHub, and `cmd/mocks`
runs them on your machine: mockoidc in Auth0's place, which signs in Ada with
the role `staff`, and a stub of GitHub, which signs in Grace, a member of the
organization `Acme` and of its team `partners`. With the demo documentation
of `demo/`, the example then starts without a cloud account and without an
Auth0 tenant. In a devbox shell, which sets `GOPRIVATE` for the library's
private module, run `make setup` once at the repository's root for the
Elements assets, and then:

    cd examples/portalapp
    go run ./cmd/mocks > mocks.env &      # the mocks, and their settings
    until grep -qs GITHUB_API_URL mocks.env; do sleep 1; done
    set -a; . ./mocks.env; set +a
    PORTAL_URL=http://localhost:8080 \
    PORTAL_BUCKET="file://$PWD/demo/docs" \
    PORTAL_ACCESS_FILE=demo/access.yaml \
    PORTAL_SESSION_KEY="$(openssl rand -base64 32)" \
    go run .

Then open http://localhost:8080. The sign-in page offers Auth0 and GitHub.
Through Auth0, Ada reads the portal Pets with its staff notes; through
GitHub, Grace reads Pets without the staff notes, and the portal Partners,
and may open the preview at http://localhost:8080/previews/pr-1. The
navigation bar ends with the reader's name and a Sign out link, which ends
the session; the signed-out page links back to the sign-in page. The test
`TestTheExampleRunsAgainstTheMocks` runs these same steps.

## How it works

At startup, portalapp reads its settings from the environment, reads the
access file, opens the bucket folder without its previews location, loads
the published documentation's snapshot, and builds its portal handler with
the access hook, the account hook `signin.AccountLinks` and, with an API key,
the chat with the reader hook `signin.ReaderID`. `source.NewReloader` checks
the folder every `PORTAL_REFRESH` and swaps in each settled change, and
`portal.WithPreviews` opens the preview folders under the previews location.
`docs/flow.dfd` draws these steps.

The sign-in package's middleware signs readers in through one identity
provider, so portalapp builds one middleware for each configured provider,
each around the previews handler, and puts the sign-in router in front:

- `/sign-in` shows the sign-in page, with a button for each provider.
- A button posts to `/sign-in/auth0` or `/sign-in/github`, which keeps the
  reader's choice in the cookie `signin_provider` (`__Host-signin_provider`
  over https) for 30 days, and returns the reader to the page of the first
  request through that provider's sign-in. A post from another site gets
  403.
- `/auth/auth0/callback` and `/auth/github/callback` go to the middleware
  of their provider, and so do `/auth/auth0/sign-out` and
  `/auth/github/sign-out`, the sign-out links of the navigation bar, which
  also delete the choice.
- Every other request goes to the middleware of the reader's choice, or
  without one to the sign-in page.

Each middleware puts its provider's name into the context of each request,
and the access hook reads it with the reader's claims from
`signin.IdentityOf`.

## The access file

The access file, `PORTAL_ACCESS_FILE` (`access.yaml` by default), is a local
file that portalapp reads at startup, never a file of the bucket folder, so
that whoever uploads documentation cannot grant themselves access. Under
`labels`, each label of the configuration file has a list of rules; under
`previews`, a list of previews rules opens every preview to each matching
reader. A rule matches a reader who signed in through its provider and
whose claim holds one of its values:

    labels:
      staff:
        - provider: auth0
          claim: https://docs.example.com/roles
          values: [staff]
        - provider: github
          claim: team_ids
          values: ["2001"] # acme/staff
      partner:
        - provider: github
          claim: team_ids
          values: ["2002"] # acme/partners
    previews:
      - provider: github
        claim: org_ids
        values: ["1001"] # acme

A portal or a section without labels opens to every signed-in reader, and
one with labels to a reader with one of them; a section needs its portal
open too. An Auth0 rule may name any claim of the ID token, and matches its
values exactly. Keep it to a claim under the administrators' control, such
as the roles claim or one from `app_metadata`: a rule on `email`, `name` or
`nickname` opens to whoever sets that claim, and Auth0 gives an email
whether or not anyone verified it.

A GitHub rule names one of the claims of the GitHub provider: `login` and
`id`, the reader's login and numeric ID; `orgs` and `org_ids`, the logins
and IDs of the reader's organizations; and `teams` and `team_ids`, each of
the reader's teams as `org/team-slug` and by its ID. It matches the names
in any letter case, as on GitHub. A name changes hands: after a rename,
another account may take an organization's or a user's old name, and with
it the rules on that name. Write rules on the IDs, which never change
hands, and keep the names in comments; `gh api orgs/acme --jq .id` gives an
organization's ID, and `gh api orgs/acme/teams/partners --jq .id` a team's.
The GitHub provider keeps in a reader's session only the organizations and
teams named by some rule, so that a reader in many teams still fits the
session cookie; after a change of the access file, a reader signs in again
for the new rules to see the reader's other memberships.

portalapp refuses to start with a missing access file, one with an unknown
key, or a rule without a provider, a claim or values.

## Auth0

Create a Regular Web Application. Add
`https://docs.example.com/auth/auth0/callback` to its Allowed Callback URLs
and `https://docs.example.com/` to its Allowed Logout URLs. Create the roles
of the access file, such as `staff` and `partner`, under User Management,
and give them to the readers. Then add a post-login Action to the Login
flow, which puts the reader's roles into the ID token under a namespace of
the portal's own, the only form of a custom claim at Auth0:

    exports.onExecutePostLogin = async (event, api) => {
      api.idToken.setCustomClaim("https://docs.example.com/roles", event.authorization?.roles ?? []);
    };

Auth0 puts a reader's RBAC permissions into the access token's
`permissions` claim, for an API, but the sign-in reads the reader from the
ID token alone: a rule on permissions needs an Action that adds them to the
ID token too. A claim from the reader's `app_metadata` suits a rule as well,
but never one from `user_metadata`, open to each reader's own edits.

The settings: `AUTH0_ISSUER`, the tenant's domain as
`https://TENANT.auth0.com/`, with the trailing slash; `AUTH0_CLIENT_ID` and
`AUTH0_CLIENT_SECRET` of the application; and `AUTH0_LOGOUT_URL`, Auth0's
logout endpoint, which ends Auth0's session too:
`https://TENANT.auth0.com/v2/logout?client_id=CLIENT_ID&returnTo=https%3A%2F%2Fdocs.example.com%2F`.

## GitHub

Register an OAuth app under Settings, Developer settings, OAuth Apps, with
the homepage URL `https://docs.example.com` and the authorization callback
URL `https://docs.example.com/auth/github/callback`, and give portalapp its
client ID and a client secret. portalapp asks for the scope `read:org`, so
that `GET /user/orgs` and `GET /user/teams` list the reader's private
memberships too. An organization that restricts OAuth apps lists itself
only after an owner approves the app. GitHub keeps its own session across a
sign-out from the portal, so the next sign-in through GitHub passes without
a password for the rest of that session.

The settings: `GITHUB_CLIENT_ID` and `GITHUB_CLIENT_SECRET`. The tests and
the local run point `GITHUB_URL` and `GITHUB_API_URL` at the stub.

## The bucket

On GCS, create a bucket and a service account with the role Storage Object
Viewer (`roles/storage.objectViewer`) on that bucket alone, and run
portalapp with that account's credentials, from the platform or from
`GOOGLE_APPLICATION_CREDENTIALS`. Name the folder in `PORTAL_BUCKET`, as
`gs://docs-bucket?prefix=portal/`. On S3, an IAM policy with
`s3:ListBucket` and `s3:GetObject` on the bucket does the same, with
`s3://docs-bucket?region=eu-west-1&prefix=portal/`. The folder holds the
configuration file, `environment.yaml` by default, with the files of its
sections, and the previews location, `previews/` by default, with a folder
for each preview, the work of a CI job.

## Settings

Each setting comes from the environment.

| Variable | Default | Holds |
|---|---|---|
| `PORTAL_ADDR` | `:8080` | the address to listen on |
| `PORTAL_URL` | required | the application URL, its origin, such as `https://docs.example.com`, before the paths of the callback URLs |
| `PORTAL_BUCKET` | required | the bucket folder's Go CDK URL |
| `PORTAL_CONFIG` | `environment.yaml` | the configuration file's path in the folder |
| `PORTAL_PREVIEWS` | `previews` | the previews location's path in the folder |
| `PORTAL_REFRESH` | `1m` | between checks of the folder; `0` for none |
| `PORTAL_ACCESS_FILE` | `access.yaml` | the access file |
| `PORTAL_SESSION_KEY` | required | 32 bytes or more of secret for the session cookies, the same for every replica |
| `PORTAL_CHAT_MODEL` | `claude-opus-5-5` | the chat's model |
| `AUTH0_ISSUER`, `AUTH0_CLIENT_ID`, `AUTH0_CLIENT_SECRET` | none | Auth0, on with any of its settings |
| `AUTH0_LOGOUT_URL` | none | the reader's destination after a sign-out from Auth0; none for the signed-out page |
| `GITHUB_CLIENT_ID`, `GITHUB_CLIENT_SECRET` | none | GitHub, on with any of its settings |
| `GITHUB_URL`, `GITHUB_API_URL` | GitHub's | GitHub's web and API hosts, for the stub |
| `ANTHROPIC_API_KEY` | none | the chat, on with the key |
| `ANTHROPIC_BASE_URL` | Anthropic's | the Messages API's host |

At least one identity provider must be on. A provider is on with any of
its settings, and then needs its client ID and secret, and Auth0 its
issuer too, so that a stray setting of a provider stops startup with its
name. portalapp refuses to start without the required settings, with a
provider whose settings are incomplete, or with an application URL that
has a path.

## Docker

Build the image from the repository's root, since the example's module
builds against the library of the checkout. The library needs the private
module `github.com/bilus/live-templ`, which Go fetches with git, so pass a
`.netrc` with a GitHub token that reads it as a build secret; no layer
keeps it:

    docker build -f examples/portalapp/Dockerfile --secret id=netrc,src=$HOME/.netrc -t portalapp .

The image holds the program alone, so mount the access file, and for GCS
outside Google Cloud the service account's key, beside the settings in a
file of `NAME=value` lines; on Cloud Run, the service's own account reads
the bucket, with no key:

    docker run -p 8080:8080 --env-file portalapp.env \
      -v "$PWD/access.yaml:/etc/portalapp/access.yaml:ro" -e PORTAL_ACCESS_FILE=/etc/portalapp/access.yaml \
      -v "$PWD/key.json:/etc/portalapp/key.json:ro" -e GOOGLE_APPLICATION_CREDENTIALS=/etc/portalapp/key.json \
      portalapp

## Tests

`devbox run make test` at the repository's root runs the example's tests
with the library's, the kept run among them, and `devbox run make test-e2e`
its browser tests, which sign two readers in through the mocks in headless
Chrome. The design, `docs/flow.dfd` of this directory, has its review page
in `docs/review/`.
