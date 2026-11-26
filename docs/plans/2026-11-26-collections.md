# Collections: list the portals under named collections

First written with the term group; renamed to collection on 2026-11-26 (stage 4).

Ledger: `docs/plans/2026-11-26-collections.ledger.md`.
Design: `docs/flow.dfd`, `docs/flow.3.dfd`; terms in `docs/vocabulary.md`.
Branch: `bilus/development`, from `c4753c5`. No issue yet.

## Requirements

1. The configuration file may hold `collections:`, a list of collections, each with a `name` and `matchLabels`, a list of labels. `portal.ReadConfig` reads them into `portal.Config.Collections`, and a program may set `Config.Collections` in code. It refuses an empty entry, a key it does not know, and `matchLabels` that is not a list of strings.
2. A portal belongs to a collection when its labels hold every match label of the collection. A portal may belong to several collections or to none.
3. `portal.New` and `portal.NewLibraries` refuse, with an error that names the collection: a collection without a name, a collection without match labels, an empty match label, and two collections of one name.
4. With collections, the home page lists the collections in their order, each as a heading with the links of its portals in the order of the portal configuration. A portal of two collections shows under both. A portal of no collection is not listed.
5. With collections, the portal menu lists the same collections, each as a label over its portals' links, and holds no portal of no collection.
6. A portal of no collection keeps its pages at `/portals/{slug}/`. Its navigation bar shows the menu of the listed portals.
7. `portal.New` writes one line to the log for each portal of no collection, and one for each collection of no portal. `NewLibraries` writes none, so that docportal with a chat logs each line once for each build.
8. The home page and the menu count the listed portals alone: with one listed portal `/` redirects to it and no page has a menu, and with none the home page says that no portals are open.
9. Collections follow the reader's access: a collection lists only its visible portals, and a collection without a visible portal is left out.
10. Custom templates get the collections as `.Collections` on the home page and `.Nav.Collections` in the bar, each with `.Name` and `.Links`. `.Portals` and `.Nav.Menu` hold the listed portals, each once, so a template without collections still hides a portal of no collection. The sample pages of the template check render collections.
11. `Library.Collections` returns the names of its portal's collections and `Library.CollectionNames` every collection's name, and the chat page's portal menu lists the portals as requirement 5 says.
12. Without collections, every page and every log is as it is today. The existing tests guard this.
13. The README, the vocabulary and the doc comments describe collections.

Out of scope: collections of sections; an access rule from a collection; collections in the example application's demo.

## Questions and assumptions

- Q1. The match rule. Decided on 2026-11-26: a portal belongs to a collection when it holds every match label. Match labels `a, b` match a portal labelled `a, b` or `a, b, c`, and not one labelled `a`, `b` or `x`.
- Q2. Items. Assumption: collections hold portals only. Sections keep their labels for the access hook.
- Q3. Where collections show. Assumption: the home page, the portal menu, and the chat page's menu (requirements 4, 5, 11).
- Q4. A portal of no collection. Assumption: not listed, but still served at its URL (requirement 6). The collections shape the lists; the access hook decides who may read.
- Q5. Where the warning goes. Assumption: the log, at each build of the handler, so docportal repeats it at each reload that changes the snapshot, as it does for a toc problem.
- Q6. A collection of no portal. Assumption: a warning in the log, since it is most likely a misspelt label, and the collection is left out of the lists.
- Q7. A collection without match labels would hold every portal. Assumption: refused as a mistake (requirement 3).
- Q8. The key is `matchLabels`, as you wrote it, although the other keys are lower-case words.
- Q9. Reviews. The defect review of AGENTS.md runs once, after the last stage's approval and before the pull request; its findings that need changes become stage 6.
- Q10. No issue exists for this change. Assumption: the pull request closes none, unless you open one.

## The change in brief

A collection is a named set of portals: the portals whose labels hold every match label of the collection. `ReadConfig` reads the collections, and `portal.New` puts each portal into its collections and writes a collection problem to the log for a portal of no collection and for a collection of no portal. With collections, the home page and the portal menu link the listed portals under each collection's name; a portal of no collection is not listed and keeps its pages. Without collections nothing changes.

## Metaphor

A building's directory board. The board (home page, portal menu) lists tenants (portals) under department headings (collections) by the tags on each tenant's door (labels). A tenant with tags of two departments is on the board twice. A tenant on no heading is missing from the board, and its office stays open at its room number.

## Stages

Each stage ends with `devbox run make build lint test test-e2e`, the hole census (`grep -rn "HOLE(" --include='*.go' .`), the score, the review page, a ledger line and a halt.

### Stage 1: the skeleton (this gate)

- Goal: the design in code: `Collection`, `Config.Collections`, box 3.5 `collectPortals` in `New`, `matchCollections`, `listPortals` behind the home page and the menu, `Library.Collections` and `Library.CollectionNames`, each body a hole; the smoke test `TestNewWithCollections` passes and ten acceptance tests are skipped.
- Requirement: 12.
- Holes added: `2 portal.ReadConfig`, `2 portal.collectPortals`, `2 portal.matchCollections`, `3 portal.listPortals`, `4 portal.Library.Collections`, `4 portal.Library.CollectionNames`, `4 chat.Chat.mount`.
- Size: 110 lines of code, 300 of tests.

### Stage 2: read and match the collections

- Goal: docportal reads `collections:`, refuses the collections of requirement 3, and logs each collection problem.
- Requirement: 1, 2, 3, 7.
- Dependencies: stage 1.
- Holes: `2 portal.matchCollections`, `2 portal.collectPortals`, `2 portal.ReadConfig`.
- Acceptance: `TestReadConfigReadsCollections`, `TestCollectionChecks`, `TestCollectionWarnings`.
- Size: 80 lines.

### Stage 3: list the portals by collection

- Goal: the home page and the portal menu list the portals by collection, for built-in and custom templates; the README describes collections.
- Requirement: 4 to 10, 13.
- Dependencies: stage 2.
- Holes: `3 portal.listPortals`. The stage also changes `home.html`, `nav.html` and the sample pages of `samplesOf`, which no hole can hold.
- Acceptance: `TestHomePageListsCollections`, `TestPortalMenuListsCollections`, `TestOneListedPortalOpensAtHome`, `TestCollectionsFollowAccess`, `TestACustomHomePageListsTheCollections`.
- Size: 150 lines: 50 of code and templates, 100 of README.

### Stage 4: rename group to collection

- Goal: a refactoring: the term, the YAML key `collections`, the Go names, the template fields, the log lines, the diagrams and the docs say collection. Behavior is unchanged.
- Requirement: all.
- Dependencies: stage 3.
- Holes: none; the three open holes are retagged for stage 5.
- Acceptance: the suite, with the tests changed in names and strings alone.
- Size: renames only.

### Stage 5: the library and the chat page

- Goal: a library names its portal's collections, and the chat page's menu lists the portals by collection.
- Requirement: 11.
- Dependencies: stage 4.
- Holes: `4 portal.Library.Collections`, `4 portal.Library.CollectionNames`, `4 chat.Chat.mount`, with `chat/page.templ` and its generated files.
- Acceptance: `TestLibraryCollections`, `TestChatPageMenuListsCollections`.
- Size: 70 lines, generated: `chat/page_templ.go`, `chat/page_live.go`.

### Stage 6: the defect review's findings

Added after the defect review of AGENTS.md, if a finding needs a change.
