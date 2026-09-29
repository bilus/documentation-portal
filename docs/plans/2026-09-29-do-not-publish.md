# Leave out the parts marked x-doNotPublish

Issue: bilus/documentation-portal#19.
Ledger: `docs/plans/2026-09-29-do-not-publish.ledger.md`.
Design: `docs/flow.dfd` and `docs/flow.3.dfd`, terms in `docs/vocabulary.md`, review page in `docs/review/index.html`.
Branch: `bilus/development`, which tracks `origin/master`. Base: `b7413f3`.

## Requirements

Restated from the issue, with the rules of `scripts/prepare_to_publish.py` in `iBiquity/conrad-api-docs`.

1. The raw spec leaves out every unpublished part, so the reader's browser never receives one:
   - a mapping whose `x-doNotPublish` list names `main`, together with its key;
   - an object in a list whose `x-doNotPublish` list names `main`;
   - the sibling `<name>` of a key `x-doNotPublish-<name>` whose list names `main`, a scalar or a whole list.
2. Parts marked only for other targets remain.
3. No `x-doNotPublish` or `x-doNotPublish-<name>` key reaches the browser.
4. A configured spec without markers is served byte for byte as before.
5. Tests cover each rule of requirement 1, a marker for another target and a spec without markers, following the cases of the script's own tests.

Out of scope: the script's second step, which cuts `info.version` to major, minor and patch for `main`, and any target other than `main`.

## Questions and assumptions

- Q1. Target. The script accepts `main` alone. Assumption: the portal always uses `main`, so nothing new is configured.
- Q2. `info.version`. Assumption: out of scope, since it hides nothing. A later issue can add it.
- Q3. Markers on parts that remain. Assumption: the portal removes every `x-doNotPublish` and `x-doNotPublish-<name>` key, since readers have no use for them.
- Q4. Formatting. A spec with markers goes through `gopkg.in/yaml.v3` as a `yaml.Node`, so its key order and comments stay, but its layout may change. A spec without markers is served untouched, which keeps requirement 4 and the existing tests.
- Q5. Where. Only the raw spec carries the content, since Elements renders what `/api/specs/{spec path}` returns. Assumption: the raw spec's handler applies the rules, and the viewer page stays as it is. The design does not change: the handler belongs to box 3.3, as decided for #13.
- Q6. Cost. The rules run on every request for the raw spec, as the reads do. The endpoints file of `conrad-api-docs` is 400 KB, and the stage measures the time. Caching is out of scope.
- Q7. Metaphor: none, as before.
- Q8. Reviews: the design and vocabulary reviews run once, before the pull request.

## The change in brief

The raw spec now leaves out its unpublished parts: the parts that an `x-doNotPublish` list marks for `main`, and the siblings that an `x-doNotPublish-<name>` key marks for it. It also drops every marker key. A configured spec without markers is served as before, byte for byte.

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
