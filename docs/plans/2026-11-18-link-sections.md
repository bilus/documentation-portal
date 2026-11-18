# Link sections: a section that links a page of the program's own

Ledger: `docs/plans/2026-11-18-link-sections.ledger.md`.
Design: agreed in conversation on 2026-11-18; terms in `docs/vocabulary.md`.
Branch: `link-sections`, from `f236743` on master.

## Requirements

1. A section may have the type `link` and a `url`. `portal.ReadConfig` reads the `url` key of a section, and a program may add `portal.Section{Title: "Support", Type: portal.LinkSection, URL: "/support"}` to a portal in code.
2. `portal.New` refuses, with an error that names the section and the problem:
   - a link section without a `url`, or with an `input` or a `toc`;
   - a spec section or a docs section with a `url`;
   - a `url` that is neither a path that starts with one `/` nor an `http` or `https` URL with a host.
3. The navigation bar of every page of a portal links each visible link section under its title, at its place in the order of the portal configuration. The link's href is the checked URL, without the base path.
4. A portal whose first visible section is a link section opens at that URL: from the home page, from the portal menu, from `/portals/{slug}/`, and from `/` with one visible portal.
5. The access hook gets a link section as the portal configuration gives it, with its URL and its labels, and a link section it hides is missing from the navigation bar. A portal whose only visible sections are link sections stays visible.
6. `Library.Sections` returns a link section with its URL, so that the chat page's bar and a program's own bar show it. `Library.Documents` and `Library.Titles` hold nothing of a link section.
7. The portal handler serves no page for a link section: `/portals/{portal}/specs/{slug}` and `/portals/{portal}/docs/{slug}/` with a link section's slug answer as for a slug of no section, and no link of a document resolves to a link section.
8. A link section follows the rules of every section for its title and its slug: a title, a slug that is not empty, and a slug that no other section of the portal shares.
9. Without a link section, every page is as it is today. The existing tests guard this.
10. The README, the vocabulary and the doc comments of `portal.Section` describe link sections, and the README shows a program that serves the linked page beside the portal handler and builds its own bar from `Library.Sections`.

Out of scope: an API for the program's routes, since a program mounts them in its own mux today; exporting the portal's navigation bar or preview banner for the program's page; a link section in the example application; a flag of docportal, which reads link sections from its configuration file with no change.

## Questions and assumptions

- Q1. The key. Decided: `url`, not `input`. Every reader of `input` takes it for a path inside the documentation root.
- Q2. The URL's forms (requirement 2). Assumption: `net/url` parses it, and it passes when it has no scheme and no host and its path starts with `/`, or when its scheme is `http` or `https` and it has a host. A query and a fragment may follow either form. `//host/x`, `javascript:…`, `mailto:…`, a relative path and an empty value are refused. The href is the parsed URL written back, so that the page holds what the check saw.
- Q3. The base path. Decided: the href carries none, as the chat's routes and the account links carry none. A program under `/docs` that serves its page under `/docs/support` writes `/docs/support`.
- Q4. A link section first (requirement 4). Decided: no special rule, so a program can make its own landing page. Assumption: `New` does not guard against a first link section whose URL is the portal's own index, such as `/portals/pets/`, which would redirect forever; the README names it.
- Q5. Access (requirement 5). The portal hides the link and nothing else. The linked page's route is the program's, so the program checks the reader's access in its own handler. The README says so.
- Q6. Previews. A reader inside a preview who follows a link section leaves the preview's pages, and the program's page shows no preview banner. No change.
- Q7. Diagrams and the score card. Assumption: the data flow diagrams stay as they are, since a link section adds no process and no flow: `openSections` checks one more type, and the routes do not change. `dfdmetrics` is not installed on this machine, and the custom templates change of 2026-11-02 updated neither the score card nor the review page; this change follows it and keeps a ledger alone.
- Q8. Reviews. The defect review of AGENTS.md runs once, after stage 2 is approved and before the pull request. Its findings that need changes become stage 3.
- Q9. Delivery. One commit per filled hole on the branch, a halt at each stage boundary, then one commit per stage and one pull request. Changed on 2026-11-18 at the user's word: both stages without a halt, one commit, pushed and tagged v0.6.0, with no pull request and so no defect review yet.
- Q10. Compatibility. The change adds a field to `portal.Section` and a section type, and changes the wording of the error for an unknown type, which now names three types. Nothing else changes for a configuration without link sections.

## The change in brief

A link section is an entry of the navigation bar that points at a URL instead of a page of the portal. The portal checks the URL when it builds the handler, lists the section in the bar and in `Library.Sections`, and asks the access hook about it as about any section. The page behind the URL, its route and its bar belong to the program.

    sections:
      - title: API
        type: spec
        input: specs/pets.yaml
      - title: Support
        type: link
        url: /support

## Stages

Each stage ends with `devbox run make build lint test test-e2e`, the hole census (`grep -rn "HOLE(" --include='*.go' .`), a ledger line and a halt.

### Stage 1: the skeleton

- Goal: the design in code: the section type `LinkSection`, the field `Section.URL` with its YAML key, the checked URL on `section`, and the three holes below, with every existing test green.
- Requirement: 1.
- Dependencies: none.
- Holes added: `2 portal.linkSection`, which `openSection` calls for a link section, `2 portal.linkURL`, `2 portal.section.pageURL` (the link case).
- Acceptance: the gate, and a census of three holes.
- Size: 40 lines.

### Stage 2: link sections

- Goal: a portal with a link section shows the link in every navigation bar and in its library, opens at it when it comes first, hides it with the access hook and refuses a link section that requirement 2 names; the README and the vocabulary describe it.
- Requirement: 1 to 10.
- Dependencies: stage 1.
- Holes: `2 portal.linkURL`, `2 portal.linkSection`, `2 portal.section.pageURL`, in this order. The fill also makes `openSection` refuse a `url` on a spec section and on a docs section, which no hole can hold without failing today's tests.
- Acceptance: `TestLinkSectionURL`, `TestLinkSectionChecks`, `TestReadConfigReadsLinkSections`, `TestLinkSections` (the bar, its order, and the href under a base path), `TestAPortalOpensAtAFirstLinkSection`, `TestHiddenLinkSectionsLeaveTheBar`, `TestLibraryLinksLinkSections` and `TestLinkSectionsHaveNoPages`.
- Size: 250 lines: 60 of code, 130 of tests, 60 of README and vocabulary.

### Stage 3: the defect review's findings

Added after the defect review of AGENTS.md, if a finding needs a change; the ledger records a decision on each finding.
