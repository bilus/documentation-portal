# Request hooks: the portal as a library, with account links in the navigation bar

Issue: bilus/documentation-portal#9.
Ledger: `docs/plans/2026-10-04-request-hooks.ledger.md`.
Design: `docs/flow.dfd`, `docs/flow.3.dfd`, `docs/flow.3.4.dfd` and `docs/flow.9.dfd`; terms in `docs/vocabulary.md`; review page in `docs/review/index.html`.
Branch: the worktree branch of issue 26, after its last commit. Base: `a67f938`, the last commit of issue 26.

## Requirements

Restated from issue 9's acceptance criteria, with requirements 3, 6 and 7 from the working assumptions of Q2, Q4 and Q3.

1. A program other than docportal imports the portal, configures it, wraps the portal handler in its own sign-in middleware and mounts it beside routes of its own in its own HTTP server, through the exported API alone. A testable example of the package `portal`, in an external test package, shows it and runs as a test.
2. `portal.Config` takes the account hook, `Account func(r *http.Request) []portal.AccountLink`, which returns the account links of a request's reader, such as the reader's name and a sign-out link. The navigation bar of every HTML page of a portal (the viewer page, the document list, the document page, their error pages, and the 404 page of a missing portal) ends with them, after the portal menu, and so does the chat page's. A link without a URL shows its label as text.
3. With account links, the home page starts with a navigation bar that holds them alone, so that a reader with no visible portal can still sign out.
4. Without the account hook, and with a hook that returns no links, every page of the portal is as it is today, byte for byte, and so is the chat page's navigation bar.
5. The portal handler calls the account hook once for each request. A chat tab shows the account links of its GET, at the GET and after the join, which has no request.
6. With an account hook, every response is private, as with an access hook.
7. A link's label shows as text, and its URL passes the page's URL filter, so that a name from an identity provider adds no markup and no script to the page.
8. `chat.Config` takes the reader hook for the question limit, and without it the chat counts by client: issue 26 delivered it, and this change documents it with the other hooks.
9. `make lint` fails when a core package imports an OAuth or OpenID Connect library or an identity provider's package. The repository has no CI workflow, so the check runs in the lint target, which the gate runs.
10. The package documentation of `portal`, `chat` and `source` describes the embedding API and its request hooks, and the README has a section on embedding the portal, with the hooks. The API stays marked as not stable.
11. docportal builds the portal through the same API. It does already: a main package can use only the exported API, so this needs no change.

Out of scope: sign-in (#8), the example application (#37), previews (#36), and the next extension points, such as branding (the issue's first open question).

## Questions and assumptions

- Q1. The account hook. Assumption: `portal.Config.Account func(r *http.Request) []portal.AccountLink`, with `type AccountLink struct { Label, URL string }`. It has no error result, like the reader hook and unlike the access hook: a hook that cannot name the reader returns no links, and the bar shows none, which hides nothing and opens nothing.
- Q2. Where the links show. Assumption: at the right end of the navigation bar, after the portal menu, on every page that draws the bar, and on the home page in a bar of its own when the hook returns links (requirement 3). A page without links draws the bar as today (requirement 4).
- Q3. Escaping (requirement 7). Assumption: the portal's templates write a label as text and a URL through html/template's URL filter, which turns a `javascript:` URL into `#ZgotmplZ`, and the chat page writes them through templ's. A reader's name from an identity provider is the reader's own text, so it may hold markup.
- Q4. Privacy (requirement 6). Assumption: with an account hook, the portal handler sends every response through the private handler of #35, since every page shows its reader's own links.
- Q5. The chat page. Assumption: as the page access of #35 and the reader ID of #26. The chat's `sessionOf` takes the account links that the portal handler found for the page's GET, from `portal.AccountLinksOf(r)`, and live-templ signs them into the page session; the chat tab's mount, at the GET and at the join, reads them from there. A chat tab keeps the links of its GET until it loads again.
- Q6. The dependency check (requirement 9). The issue's technical note asks `go list -deps` over the core packages to list no package of #8, and neither `github.com/coreos/go-oidc` nor `golang.org/x/oauth2`. Two facts bound it. docportal's GCS driver imports `golang.org/x/oauth2`, through `gocloud.dev/gcp`, for the bucket's own credentials, and its S3 driver imports the AWS SDK's `ssooidc` for the AWS credential chain. And #8 plans flags of docportal that turn sign-in on, which would import #8's package. Assumption: the core packages are the packages of the embedding API with the chat's model, `portal`, `source`, `chat` and `anthropicmodel`; the check runs `go list -deps` over them and over docportal's own imports except the GCS and S3 drivers, so that docportal adds no sign-in library of its own until #8 changes the check. It refuses `golang.org/x/oauth2`, `github.com/coreos/go-oidc`, `github.com/zitadel/oidc`, `github.com/markbates/goth`, and the packages of `github.com/auth0` and `github.com/okta`; #8's package joins the list when #8 creates it. The repository has no CI workflow, so the check runs in `make lint`, which the gate runs.
- Q7. The example application of #37 does not exist yet. The testable example of `portal` stands in for requirement 1, and the criterion that #37 uses nothing but the documented API waits for #37.
- Q8. Compatibility before 1.0 (the issue's second open question). Assumption: none; the package documentation keeps saying that the API is not stable yet.
- Q9. Design. `flow.3.4.dfd` gains box 3.4.4, between 3.4.1 and 3.4.2: ask the account hook for the reader's account links (`router.accountLinks`), with `> request` and `< account links` to `<Embedding program>`. Box 3.4.2 takes the access and the account links, the reader's view carries the links into box 3.4.3, whose pages put them into each navigation bar, and box 3.4.3 writes the response private with an access hook or an account hook. The access passes through box 3.4.4, which does not use it. Box 3.3 of `flow.3.dfd` names the router's hooks together, so that a later hook leaves it as it is. The chat tab's links travel through the page session, which box 3.4.3 sends out in the page's response and gets back at the join, and flow.3.4.dfd's header says so. `flow.dfd` stays as it is, since docportal sets no hook.
- Q10. Metaphor: none, as for the earlier issues.
- Q11. Reviews: the design review and the vocabulary review at the plan gate, by sub-agents; the defect review of AGENTS.md once, after the last stage.
- Q12. Delivery: one commit per filled hole, no pull request and no push; the main session reshapes the history and opens one pull request for this issue, after the one of #26. The user lifted the halts of law 5.

## The change in brief

An embedding program wraps the portal handler in its own sign-in middleware and mounts it beside routes of its own, through the exported API, and extends it with request hooks: the access hook of #35, the reader hook of #26, and the account hook of this change, which returns the account links of each request's reader. The portal handler asks the account hook once for each request, beside the access hook, and the reader's view carries the links to every page, whose navigation bar ends with them; a chat tab keeps the links of its GET in its page session. With an account hook, every response is private. `make lint` keeps OAuth and OpenID Connect libraries and identity providers' packages out of the core packages, and the package documentation and the README show a program that embeds the portal with its hooks.

## Stages

Each stage ends with `devbox run make build lint test test-e2e`, the hole census (`grep -rn "HOLE(" --include='*.go' .`), the score card, the review page and a ledger line in place of the halt.

The plan gate declares the account hook, `Config.Account`, the type `AccountLink`, `AccountLinksOf`, the router's account hook and `router.accountLinks`, the view's links and the navigation bar's `Account`, and the chat page's `Account` with `readAccountLinks`. `ServeHTTP`, the diagram in code of process 3.4, asks both hooks and builds the view from their answers, and `viewFor` keeps the links, inline. `render` takes the request, inline at each of its calls, and holds a hole for the links; so do `route`, for the private handler, and the chat's `sessionOf`. The mocks give no links and keep today's header, so every page stays as it is. The gate writes the acceptance tests, skipped with their tags, the smoke test of process 3.4 with an account hook, the guards of today's bars, and the example of requirement 1 without its output, in whose place a hole comment stands.

### Stage 1: the account links

- Goal: the navigation bar of every page of a portal and of the chat page ends with the account links of the account hook, the home page shows them in a bar of its own, every response is private with an account hook, and every bar stays as it was without links.
- Requirement: 2 to 7.
- Dependencies: none.
- Holes: `1 portal.router.accountLinks`, `1 portal.render`, `1 portal.router.route`, `1 portal.AccountLinksOf`, `1 chat.Chat.sessionOf`, `1 chat.readAccountLinks`.
- Acceptance: `TestAccountLinksEndEveryNavigationBar`, `TestTheHomePageShowsTheAccountLinks`, `TestAccountLinksAreText`, `TestResponsesArePrivateWithAnAccountHook`, `TestTheAccountHookAnswersOncePerRequest`, `TestAccountLinksOf`, `TestAChatTabShowsTheAccountLinksOfItsGET`, `TestReadAccountLinks`, and the browser test `TestReaderSeesTheAccountLinks` in `e2e`. The guards `TestWithoutAccountLinksEveryBarIsAsBefore` and `TestWithoutAccountLinksTheChatBarIsAsBefore`, and the smoke test `TestAnAccountHookWithoutLinksChangesNoPage`, pass on the mocks.
- Size: 300 lines, with the templates and the tests.

### Stage 2: the embedding API's documentation and the dependency check

- Goal: `make lint` refuses a sign-in library in the core packages, and the package documentation, the README and a testable example show a program that embeds the portal with its request hooks.
- Requirement: 1, 8, 9, 10 and 11.
- Dependencies: stage 1.
- Holes: `2 portal_test.ExampleNew`, the example's output.
- Acceptance: `ExampleNew` with its output, and `make lint`, which passes on the tree and fails in scratch copies whose `portal` or docportal imports `golang.org/x/oauth2` (an experiment, recorded in the ledger).
- Size: 250 lines, with the README.

### Stage 3: the defect review's findings

Added after the defect review of AGENTS.md; the ledger records a decision on each finding.

- Goal: make lint fails when go list fails, the tests pin the home page's bytes and its bar's place, a private response with a hook that returns no links, a partial decode of the chat tab's links and the portal menu's place, and the documentation names the URLs that work on every page; the reviewer's wrong implementations each fail a test or the lint experiment.
- Requirement: 3, 4, 5, 6, 7 and 9.
- Dependencies: stage 2.
- Holes: none; each fix changes a recipe, a test or a doc comment.
- Acceptance: the new tests and the experiment of the ledger's decisions.
- Size: 100 lines.
