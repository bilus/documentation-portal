# Extension fields: a program's own fields on a portal

Ledger: `docs/plans/2026-11-26-extension-fields.ledger.md`.
Design: agreed in conversation on 2026-11-26; `docs/flow.dfd` unchanged; terms in `docs/vocabulary.md`.
Branch: `bilus/development`, from `6b5f3b2`, the tip of the link portals change. No issue yet.

## Requirements

1. A portal of the configuration file may hold fields whose keys start with `x-`. `portal.ReadConfig` keeps them in `Portal.Ext`, a `map[string]any`, by their keys, with their values as YAML gives them: a string, a number, a boolean, a list or a map with string keys. A portal without such fields has a nil `Ext`.
2. `ReadConfig` still refuses every other unknown key of a portal, an `x-` key anywhere else in the file (the top level, a section, a collection), the key `x-` alone, a key repeated in one portal, and a value with a map key that is not a string.
3. `Portal.GetExt[T]()` returns the extension fields as a `T`, a type of the program whose fields name their keys in yaml tags:

        type PortalExt struct {
            Host string `yaml:"x-host"`
        }
        ext, err := p.GetExt[PortalExt]()

   It returns T's zero value for a portal without extension fields, and an error that names the portal for an extension field that T lacks and for a value that does not fit T's field. It works the same for an `Ext` set in code. The result shares nothing with the portal: a write to it does not change `Ext`.
4. The portal reads none of the extension fields. The access hook, `Library.Portal` and every other place that gives a program a `Portal` give it with its `Ext`. A link portal and a portal with sections may both have them.
5. The chat's digest of a portal changes with its extension fields, so that a page's stored access closes after such a change, as after any change of the portal.
6. Without extension fields, every configuration reads as before, and `Portal` values compare as before. The existing tests guard this.
7. The README, the vocabulary and the doc comments describe extension fields.

Out of scope: extension fields on sections and collections; showing them in the built-in or custom templates; checking them against a type at startup.

## Questions and assumptions

- Q1. `GetExt`'s form. Decided: a method of `Portal`, not a function. Assumption: a generic method, `GetExt[T any]() (T, error)`, which Go 1.27 compiles and the module already requires. The alternative is `GetExt(v any) error`, in the form of `json.Unmarshal`.
- Q2. Strictness (requirement 3). Assumption: `GetExt` refuses an `x-` field that T lacks, so that a misspelt key in the file fails where the program reads it. A program that wants every field takes `map[string]any` as T, or reads `Ext`.
- Q3. When a wrong field shows. `ReadConfig` knows no T, so a misspelt `x-hots` passes it and fails at the first `GetExt`. A program that wants the failure at startup calls `GetExt` on every portal after `ReadConfig`; the README shows that loop.
- Q4. Where the fields are read. Not answered yet. Assumption: in the program's own code and in its access hook. Custom templates get nothing new; say so if a template needs them.
- Q5. Portals only. Sections and collections refuse `x-` keys as any unknown key, until a need shows.
- Q6. A program that sets `Ext` in code may use any key. Only `ReadConfig` enforces `x-`.
- Q7. Reviews. The defect review of AGENTS.md runs once, on `6b5f3b2..tip`, after the last stage's approval.
- Q8. Delivery. A third pull request, stacked on #47.

## The change in brief

Extension fields are a portal's fields of the configuration file whose keys start with `x-`. `ReadConfig` keeps them in `Portal.Ext` and refuses every other unknown key as before. The portal reads none of them and hands them on with the portal, and a program decodes them into its own type with `Portal.GetExt`.

    portals:
      - name: Foo
        type: link
        url: https://foo.example.com/
        x-host: foobar

## Metaphor

A luggage tag with a blank back. The printed front (name, type, url, sections, labels) is the carrier's form, and it rejects a box it does not print. The back takes any note that starts with `x-`. The carrier (the portal) never reads the back and hands the tag over whole; the owner (the program) reads it with a template of its own (`GetExt`).

## Stages

Each stage ends with `devbox run make build lint test test-e2e`, the hole census, the score, the review page, a ledger line and a halt.

### Stage 1: the skeleton (this gate)

- Goal: the design in code: `Portal.Ext` and `Portal.GetExt`, with the bodies as holes. The smoke test `TestNewWithExtensionFields` and `TestAPortalsExtensionFieldsChangeItsDigest` pass; five acceptance tests are skipped.
- Requirement: 5, 6.
- Holes added: `2 portal.ReadConfig`, `2 portal.Portal.GetExt`.
- Diagrams: unchanged. `ReadConfig` (box 7) reads more keys of the same file, and `GetExt` is called by the program.
- Size: 20 lines of code, 150 of tests.

### Stage 2: read and decode the extension fields

- Goal: `ReadConfig` keeps a portal's `x-` fields, and `GetExt` decodes them into the program's type; the README describes them.
- Requirement: 1 to 4, 7.
- Dependencies: stage 1.
- Holes: `2 portal.ReadConfig`, `2 portal.Portal.GetExt`.
- Acceptance: `TestReadConfigReadsExtensionFields`, `TestReadConfigStillRefusesOtherKeys`, `TestGetExt`, `TestGetExtLeavesThePortalAsItWas`, `TestTheAccessHookGetsTheExtensionFields`.
- Size: 90 lines: 50 of code, 40 of README.

### Stage 3: the defect review's findings

Added after the defect review of AGENTS.md, if a finding needs a change.
