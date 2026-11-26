# Link portals: a portal that links a page outside the portal handler

Ledger: `docs/plans/2026-11-26-link-portals.ledger.md`.
Design: `docs/flow.dfd`, unchanged; terms in `docs/vocabulary.md`.
Branch: `bilus/development`, from `be2f627`, the tip of the collections change. No issue yet.

## Requirements

1. A portal may have the type `link` and a `url`. `portal.ReadConfig` reads the `type` and `url` keys of a portal, and a program may add `portal.Portal{Name: "Status", Type: portal.LinkPortal, URL: "https://status.example.com/"}` in code.
2. `portal.New` refuses, with an error that names the portal and the problem: a link portal without a `url` or with sections; a `url` that is neither a path that starts with one `/` nor an `http` or `https` URL with a host, by the rule of link sections; a `url` on a portal without the type; and a type other than `link`.
3. The home page and the portal menu link each listed link portal under its name, at its place in the order of `portals:`. The href is the checked URL, without the base path.
4. A link portal belongs to collections by its labels, like any portal, and gets the same warning in no collection. Every collection lists its portals in the order of `portals:` in the configuration file, link portals included.
5. The portal handler serves no page for a link portal: `/portals/{slug}` and every path under it answer as for a slug of no portal, and so does a chat route there.
6. The access hook gets a link portal as the portal configuration gives it, with its type, URL and labels. A link portal is visible when the hook allows it; it needs no visible section.
7. A link portal follows the rules of every portal for its name and its slug: a name, a slug that is not empty, and a slug that no other portal shares.
8. A link portal counts as a listed portal: with one listed portal that is a link portal, `/` redirects to its URL, and two listed portals give a menu.
9. `NewLibraries` returns no library for a link portal, so the chat has no agent and no page for it. `Library.Menu(access)` returns the portal menu as the pages of the library's portal show it, link portals and collections included, each link marked as a link portal's or not. The chat page's menu is that menu limited to the chat's portals and the link portals, kept in the page's session from the page's GET.
10. Without a link portal, every page is as it is today. The existing tests guard this.
11. The README, the vocabulary and the doc comments of `portal.Portal` describe link portals.

Out of scope: a link target of a new tab; an icon or a description for a link; link portals in the example application's demo.

## Questions and assumptions

- Q1. Sections. Today a portal without sections is refused. Assumption: a link portal must have none, and a portal with sections may not have a `url`.
- Q2. The type's values. Assumption: `link` or absent. No name for the ordinary type, so `type: portal` is refused.
- Q3. The URL's forms. Assumption: the rule of link sections, with the same function: a path that starts with one `/`, or an `http` or `https` URL with a host. `foobar`, `//foobar` and `javascript:` URLs are refused. Your example `https://foobar` passes.
- Q4. The base path. Assumption: the href carries none, as for link sections.
- Q5. `/portals/{slug}/` of a link portal. Assumption: 404 as for a slug of no portal, not a redirect to the URL, as a link section has no page.
- Q6. One listed portal that is a link portal (requirement 8). Assumption: `/` redirects to its URL, as a portal opens at a first link section. The alternative is to show the home page with one link.
- Q7. The slug rules (requirement 7). A link portal has no URL of its own, so the slug serves only to refuse two portals of one name. Assumption: keep the rules, for one rule for every portal.
- Q8. The chat page's menu. Today package chat builds it from the libraries, which a link portal lacks. Assumption: `Library.Menu` gives the chat the portal's own menu, and chat's `menuOf` of the collections change goes away. `Library.Collections` and `Library.CollectionNames` stay.
- Q9. Reviews. The defect review of AGENTS.md runs once, on this change alone (`be2f627..tip`), after the last stage's approval.
- Q10. Pull requests. Assumption: two, collections first and link portals second, both opened when you say so, since neither has an issue.

## The change in brief

A link portal is an entry of the home page and the portal menu that points at a URL instead of a set of sections. The portal checks the URL when it builds the handler, lists the link portal at its place among the portals and in its collections, and asks the access hook about it as about any portal. It has no pages, no chat page and no library; the page behind the URL belongs to the program or to another site.

    portals:
      - name: Foobar
        type: link
        url: https://foobar
        labels: [xyz]

## Metaphor

The building's directory board again. A link portal is a line on the board that names a tenant in another building and gives its street address. The board (home page, portal menu) lists it among the others, under the same department headings (collections), and the building has no room for it (no pages).

## Stages

Each stage ends with `devbox run make build lint test test-e2e`, the hole census, the score, the review page, a ledger line and a halt.

### Stage 1: the skeleton (this gate)

- Goal: the design in code: `PortalType`, `LinkPortal`, `Portal.Type`, `Portal.URL`, both read by `ReadConfig`, `linkPortal` behind `openPortals`, `site.link`, `Library.Menu`, `PortalLink` and `MenuCollection`, with the bodies as holes. The smoke test `TestNewWithALinkPortal` and `TestReadConfigReadsLinkPortals` pass; nine acceptance tests are skipped.
- Requirement: 1, 10.
- Holes added: `2 portal.openPortals`, `2 portal.linkPortal`, `2 portal.site.url`, `2 portal.site.index`, `2 portal.site.visibleTo`, `2 portal.view.portalFor`, `3 portal.NewLibraries`, `3 portal.Library.Menu`, `3 chat.Chat.mount`.
- Diagrams: unchanged. `openPortals` (box 3.1) checks one more type, and `linkPortal` is a helper inside its box.
- Size: 60 lines of code, 230 of tests.

### Stage 2: link portals in the lists

- Goal: a link portal shows on the home page, in the portal menu and in its collections at its place, hides with the access hook, has no pages, and is refused as requirement 2 says; the README describes it.
- Requirement: 2 to 8, 11.
- Dependencies: stage 1.
- Holes: `2 portal.linkPortal`, `2 portal.openPortals`, `2 portal.site.url`, `2 portal.site.index`, `2 portal.site.visibleTo`, `2 portal.view.portalFor`.
- Acceptance: `TestLinkPortalChecks`, `TestLinkPortalsAreListed`, `TestLinkPortalsKeepTheirPlaceInACollection`, `TestALinkPortalHasNoPages`, `TestLinkPortalsFollowAccess`, `TestOneListedLinkPortalOpensAtHome`.
- Size: 110 lines: 50 of code, 60 of README.

### Stage 3: the library's menu and the chat page

- Goal: a link portal has no library, a library gives its portal's menu, and the chat page's menu holds the link portals.
- Requirement: 9.
- Dependencies: stage 2.
- Holes: `3 portal.NewLibraries`, `3 portal.Library.Menu`, `3 chat.Chat.mount`, which removes chat's `menuOf`.
- Acceptance: `TestLibrariesLeaveOutLinkPortals`, `TestLibraryMenu`, `TestChatPageMenuListsLinkPortals`; the chat menu tests of the collections change pass unchanged.
- Size: 60 lines.

### Stage 4: the defect review's findings

Added after the defect review of AGENTS.md, if a finding needs a change.
