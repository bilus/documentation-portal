# Collection access: extension fields on a collection, and an access that hides collections

Ledger: `docs/plans/2026-11-26-collection-access.ledger.md`.
Design: `docs/flow.3.4.dfd`, box 3.4.2 changed; terms in `docs/vocabulary.md`.
Branch: `bilus/development`, from `bb158a7`, the tip of the extension fields change (pull request 48, still open). No issue yet.

## Requirements

1. A collection of the configuration file may hold fields whose keys start with `x-`. `portal.ReadConfig` keeps them in `Collection.Ext`, a `map[string]any`, with the value rules and the refusals of a portal's extension fields: any other unknown key, the key `x-` alone, a repeated key, a map key that is not a string, `.nan` and `.inf`. A collection without such fields has a nil `Ext`.
2. `Collection.GetExt[T]()` returns the extension fields as a `T`, with the contract of `Portal.GetExt`; its errors name the collection. `Collection.Clone()` returns a copy that shares nothing with the collection.
3. An access may also decide which collections its reader sees, by implementing `portal.CollectionAccess`:

        type CollectionAccess interface {
            Access
            Collection(c Collection) bool
        }

   The portal handler asks it about each collection of the portal configuration, extension fields included, once for each request, each time with a clone of its own. An access without the method shows every collection, as today.
4. A hidden collection is absent from the home page, from the portal menu and from `Library.Menu`, and so from the chat's menu. The code that builds the reader's view filters the collections; the home template and the nav template stay as they are and receive only shown collections.
5. A portal is listed for a reader when `Access` allows it and it is in at least one collection shown to the reader. `Access.Portal` and `Access.Section` alone decide whether a portal's pages are served: a hidden portal appears in no list and its pages answer 404; a portal whose every collection is hidden is not listed and keeps its pages, as a portal of no collection does today.
6. With collections of which none is shown, no portal is listed, and the home page says that no portal is open. With one listed portal that is no link portal, `/` redirects to it, as today.
7. The portal handler keeps clones of the collections, so that a later write to the program's configuration reaches no request.
8. A test gives portals and collections the field `x-roles`, the roles allowed to see each, and runs a hook for readers with two different roles: each reader sees the collections and the portals of the reader's role on the home page and in the portal menu, opens the pages of those portals, and gets 404 for the others.
9. Without a collection access and without collection extension fields, everything behaves as before. The existing tests guard this, with one exception (Q4).
10. The README, the vocabulary and the doc comments describe both.

Out of scope: extension fields on sections; role rules in `examples/portalapp` and in docportal; a collection access for previews.

## Questions and assumptions

- Q1. The hook's form. Assumption: an optional interface, `CollectionAccess`, in the form of `PreviewAccess`, so that every existing `Access` compiles and behaves as before. The alternative adds `Collection` to `Access` itself, which breaks each existing implementation at compile time: `readerAccess` of the example, `fakeaccess`, the chat's page access and those of embedding programs. Its advantage: no access can lack the answer by accident, such as a wrapper that drops the method.
- Q2. What hiding a collection closes (requirement 5). Assumption: the list only. The program closes a portal with `Access.Portal`, as the roles test does with `x-roles` on the portal. The alternative closes the pages of a portal whose every collection is hidden, which gives two rules for one portal, and a portal in a shown and a hidden collection would need a third.
- Q3. The default. `portal.Everything` and an access without the method show every collection; after a hook error no portal is visible, so no collection is.
- Q4. One existing test case changes in stage 2: `TestReadConfigStillRefusesOtherKeys` expects that `ReadConfig` refuses `x-host` on a collection, which requirement 1 reverses. Stage 2 removes that one case; the top-level and section cases stay. This needs sign-off with the plan.
- Q5. The chat. A chat page keeps its menu from the page's GET, built with the reader's access, so it follows the collection access with no change to package chat. Stage 3 confirms it with a test under the build tag `chat`.
- Q6. `Library.Collections` and `Library.CollectionNames` keep returning the configuration's collections, not a reader's, since a library is not a reader's view. `Library.Menu(access)` filters.
- Q7. Reviews. The defect review of AGENTS.md runs once, on `bb158a7..tip`, after the last stage's approval.
- Q8. Delivery. A fourth pull request, opened when the change is complete; it stacks on 48 unless 48 has merged by then.

## The change in brief

A collection gains extension fields: `ReadConfig` keeps its `x-` keys in `Collection.Ext`, and `Collection.GetExt` decodes them into the program's type. A collection access is an access with a third answer, `Collection(c)`. When the portal handler builds the reader's view, it asks the collection access about each collection once and leaves on the reader's sites only the shown collections, so the home page and the portal menu list a portal under the shown collections alone, and a portal of no shown collection is not a listed portal.

    collections:
      - name: Internal
        matchLabels: [internal]
        x-roles: [staff]

## Metaphor

A building directory in a lobby. Each floor (a collection) lists its offices (portals), and the guard at the desk (the collection access) folds away the floors a visitor has no business on before handing over the sheet. The sheet printer (the templates) prints what it receives. The office doors have their own locks (`Access.Portal`): a folded floor removes an office from the sheet and leaves its door as it was.

## Stages

Each stage ends with `devbox run make build lint test test-e2e`, the hole census, the score, the review page, a ledger line and a halt.

### Stage 1: the skeleton (this gate)

- Goal: the design in code: `Collection.Ext`, `Collection.Clone`, `Collection.GetExt`, `CollectionAccess` and `showCollections`, with the bodies as holes. The smoke test `TestNewWithACollectionAccess` passes; seven acceptance tests are skipped.
- Requirement: 9.
- Holes added: `2 portal.ReadConfig`, `2 portal.Collection.Clone`, `2 portal.Collection.GetExt`, `3 portal.showCollections`.
- Diagrams: box 3.4.2 of `docs/flow.3.4.dfd` also asks the embedding program about each collection. `showCollections` is a call inside that leaf box's function, `viewFor`, through `visibleSites`, so it has no box of its own.
- Size: 45 lines of code, 330 of tests.

### Stage 2: a collection's extension fields

- Goal: `ReadConfig` keeps a collection's `x-` fields, `Collection.GetExt` decodes them, and `Collection.Clone` copies a collection whole.
- Requirement: 1, 2.
- Dependencies: stage 1.
- Holes: `2 portal.ReadConfig`, `2 portal.Collection.Clone`, `2 portal.Collection.GetExt`.
- Acceptance: `TestReadConfigReadsACollectionsExtensionFields`, `TestCollectionGetExt`, `TestACollectionsCloneSharesNothing`; `TestReadConfigRefusesOtherKeysOfACollection` keeps passing. One case leaves `TestReadConfigStillRefusesOtherKeys` (Q4).
- Size: 50 lines of code. `Portal.GetExt` and `Collection.GetExt` share one unexported decoder.

### Stage 3: the collection access hides collections

- Goal: a reader's home page and portal menu hold only the collections that the reader's collection access shows; the roles test passes; the README describes extension fields on collections and the collection access.
- Requirement: 3 to 8, 10.
- Dependencies: stage 2.
- Holes: `3 portal.showCollections`.
- Acceptance: `TestRolesOpenCollectionsAndPortals`, `TestAHiddenCollectionLeavesItsPortalsPages`, `TestEachCollectionCallGetsItsOwnCollection`, `TestLibraryMenuHidesACollection`, and a new test of the chat's menu (Q5).
- Size: 60 lines of code, 40 of tests, 40 of README.

The defect review follows stage 3, and a stage 4 takes its findings if any need changes.
