# Leave out the parts marked x-doNotPublish

Issue: bilus/documentation-portal#19.
Ledger: `docs/plans/2026-09-29-do-not-publish.ledger.md`.
Design: `docs/flow.dfd` and `docs/flow.3.dfd`, terms in `docs/vocabulary.md`, review page in `docs/review/index.html`.
Branch: `bilus/development`, which tracks `origin/master`. Base: `b7413f3`.

## Requirements

Restated from the issue, with the rules of `scripts/prepare_to_publish.py` in `iBiquity/conrad-api-docs`.

1. The raw spec and the viewer page leave out every unpublished part, so the reader's browser never receives one:
   - a mapping whose `x-doNotPublish` list names `main`, together with its key;
   - a list element whose `x-doNotPublish` list names `main`;
   - the sibling `<name>` of a key `x-doNotPublish-<name>` whose list names `main`, a scalar or a whole list.
2. Parts marked only for other targets remain.
3. No `x-doNotPublish` or `x-doNotPublish-<name>` key reaches the browser.
4. A configured spec from which the rules remove nothing is served byte for byte as before, and every YAML document of the file stays.
5. Tests cover each rule of requirement 1, a marker for another target and a spec from which the rules remove nothing, following the cases of the script's own tests.

Out of scope: the script's second step, which cuts `info.version` to major, minor and patch for `main`, and any target other than `main`.

## Questions and assumptions

- Q1. Target. The script accepts `main` alone. Assumption: the portal always uses `main`, so nothing new is configured.
- Q2. `info.version`. Assumption: out of scope, since it hides nothing. A later issue can add it.
- Q3. Markers on parts that remain. Assumption: the portal removes every `x-doNotPublish` and `x-doNotPublish-<name>` key, since readers have no use for them.
- Q4. Formatting. A spec with a removed part goes through `gopkg.in/yaml.v3` as a `yaml.Node`: its key order stays, most comments stay, and its layout may change. A comment attached to a removed part leaves with it, and so may a foot comment of the mapping that held it.
- Q5. Where. Changed in stage 2: loading the spec applies the rules, so the viewer page's title and the raw spec both come from the published spec. The design does not change: the handlers and their helpers belong to box 3.3, as decided for #13.
- Q6. Cost. The rules parse the spec on every request for the raw spec or the viewer page, a spec without markers too, since a text search misses an escaped marker key such as `"x-doNot\x50ublish"`. On the 400 KB endpoints file of `conrad-api-docs`, publishedSpec takes 13 ms with the file's marker and 6.4 ms without it, beside the 6.3 ms of the parse that loads the title; a request for the raw spec takes 21 to 25 ms. Caching is out of scope.
- Q7. Metaphor: none, as before.
- Q9. References. Decided at stage 2, for parity with the script: a `$ref` or a `required` entry that names a removed part stays.
- Q10. The root. Decided at stage 2, for parity with the script: a marker on the spec's root mapping removes nothing but its own key.
- Q11. YAML aliases. Decided at stage 2: every alias is expanded into a copy before the rules apply, so a marker given through an alias counts and no alias points at a removed anchor; the output holds no anchors and no aliases. The expansion fails after 100,000 copied nodes, as yaml.v3 fails on excessive aliasing, so that nested aliases or an alias inside its own anchor cannot exhaust the memory or the stack. Merge keys (`<<`) stay out of scope: a marker in a merge source applies inside the source only.
- Q12. The marker's value. Decided at stage 2: a list that holds `main`, or the scalar `main`. A mapping or any other value names no target.
- Q8. Reviews: the design and vocabulary reviews run once, before the pull request.

## The change in brief

The raw spec and the viewer page now leave out the unpublished parts of the configured spec: the parts that an `x-doNotPublish` value marks for `main`, and the siblings that an `x-doNotPublish-<name>` key marks for it. The raw spec also drops every marker key. A configured spec from which the rules remove nothing is served as before, byte for byte.

## Stages

Each stage ends with `devbox run make build lint test test-e2e`, the hole census (`grep -rn "HOLE(" --include=*.go .`), the score card, the review page and a halt for review.

The skeleton adds the call in the raw spec's handler, as one hole whose mock returns the spec unchanged. That is the right result for every spec without markers, so all existing tests stay green.

### Stage 1: the rules

- Goal: `/api/specs/{spec path}` leaves out the unpublished parts of the configured spec and every marker key.
- Requirement: 1, 2, 3, 5.
- Dependencies: none.
- Holes: `1 portal.publishedSpec`.
- Acceptance: `TestPublishedSpec`, `TestRawSpecLeavesOutUnpublishedParts`.
- Size: 120 lines.

### Stage 2: the review's findings

- Goal: the tests pin every rule, the viewer page uses the published spec, aliases and multi-document files work, and a spec from which nothing is removed is served byte for byte.
- Requirement: 1 to 5.
- Dependencies: 1.
- Holes: none; each change inside `publishedSpec`, its helpers and `site.loadSpec` starts as a failing test.
- Acceptance: `TestPublishedSpec`, `TestPublishedSpecKeepsBytesWhenNothingIsRemoved`, `TestPublishedSpecRejectsExcessiveAliasing`, `TestViewerPageLeavesOutUnpublishedTitle`, and each of the design review's 12 wrong fills failing at least one test.
- Size: 100 lines.

