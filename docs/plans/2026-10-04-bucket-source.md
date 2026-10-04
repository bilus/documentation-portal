# Bucket source: the documentation from a GCS or S3 folder, held in memory as one snapshot

Issue: bilus/documentation-portal#34.
Ledger: `docs/plans/2026-10-04-bucket-source.ledger.md`.
Design: `docs/flow.dfd`, `docs/flow.3.dfd` and, from stage 2, `docs/flow.9.dfd`; terms in `docs/vocabulary.md`; review page in `docs/review/index.html`.
Branch: `bilus/development`, which tracks `origin/master`. Base: `5f4f107`.

## Requirements

Restated from issue 34's acceptance criteria.

1. docportal, and a program that uses the library, can serve the documentation from a bucket folder named by a Go CDK URL, such as `gs://docs-bucket?prefix=portal/`, `s3://docs-bucket?region=eu-west-1&prefix=portal/` or `file:///srv/docs?prefix=portal/`. A local directory keeps working as today.
2. The configuration file comes from the folder, and its input and toc paths are relative to the folder.
3. docportal loads the whole folder into memory at startup, and stops with an error when it cannot read the folder or the configuration file, or when the portal configuration fails the checks of `portal.New`.
4. At the refresh interval, the portal checks the folder for changes. A change that two checks in a row report alike is a settled change: the portal loads a new snapshot and replaces the old one in one step. A request that started on the old snapshot finishes on it.
5. A failed check, and a failed refresh, from a failed read of the folder or a configuration that `portal.New` refuses, keeps the old snapshot in service and writes the cause to the log. The next check tries again.
6. The chat answers from the snapshot in service, and a refresh keeps its conversations and its question counts.
7. The portal refuses a folder over the size limit: at startup with an error, at a refresh by keeping the old snapshot.
8. The portal needs only read access to the bucket. Credentials come from the platform's usual sources, never from the configuration file or the flags.
9. The tests run against `memblob` and `fileblob`, with no cloud account.

Out of scope: preview folders (#36), git sources (#4, #5), and a refresh of a local directory, which the portal reads live as today.

## Questions and assumptions

- Q1. Settling. Assumption: a changed listing is loaded only when the next check reports the same listing, so that an upload in progress, which changes the listing from check to check, is not loaded half done. A marker object and a pointer object, the issue's alternatives, wait for a need.
- Q2. Defaults. Assumption: a refresh interval of one minute and a size limit of 256 MiB.
- Q3. Flags. Assumption: `-root` names the bucket folder by its URL, and `-config` then names the configuration file by its path inside the folder, `environment.yaml` by default; without `-root`, `-config` names a local file whose directory is the documentation root, as today. `-refresh` sets the refresh interval, and `-max-size` the size limit in MiB. The environment variables are `DOCPORTAL_ROOT`, `DOCPORTAL_REFRESH` and `DOCPORTAL_MAX_SIZE`.
- Q4. Drivers. Assumption: docportal registers the GCS, S3 and file drivers of Go CDK. A program that uses the library registers the drivers it needs with a blank import, as Go CDK intends, and the README says so.
- Q5. The snapshot. Assumption: the files of the folder go into a `testing/fstest.MapFS`, which passes `fstest.TestFS` and lists a directory by scanning every file, which is fast enough for a documentation folder. The `*blob.Bucket` itself is never used as an `fs.FS`, because `Bucket.Sub` closes the bucket it is called on (Go CDK v0.46.0, `blob/blob_fs.go:229`), and the portal calls `fs.Sub` on its root for every docs section.
- Q6. The listing. Assumption: a listing holds every object of the folder with its key, size, modification time and MD5 sum, as the bucket reports them, in key order; it leaves out the zero-byte objects whose keys end in `/`, which the GCS console writes for a folder, and refuses a key that is not a valid `io/fs` path. Two listings are equal when their entries are equal item for item.
- Q7. The local directory. Assumption: it is a documentation source too, whose listing is empty and whose snapshot is the directory opened with `os.OpenRoot` and read live, so that startup is one flow for both kinds of source, and the reloader never finds a change in it.
- Q8. The chat across snapshots. Assumption: one `chat.Chat` serves every snapshot: a refresh gives it the new snapshot's libraries with `Chat.Reload`, which rebuilds its agents and keeps its sessions, conversations and question counts, and `Chat.Routes` builds the pages of the new libraries. A page open before the refresh keeps its socket to the old live app until it reconnects, and its questions go to the new agents, since `Ask` looks the agent up by the portal's slug. A portal that a refresh removes answers its questions with `ErrNoPortal`.
- Q9. Design. The top diagram keeps its boxes and their numbers, and `main.startup` keeps its order: box 2 opens the documentation source in place of the directory, box 8 loads the source's snapshot, boxes 7, 5, 6 and 3 build the portal handler from the snapshot as today, and box 9 serves it and rebuilds it for each settled change with the builder, a function that runs boxes 7, 5, 6 and 3 again. Box 6 keeps the chat in a store across the builds. `flow.3.dfd` keeps its boxes, and its exit entity becomes box 9. Stage 2 draws `flow.9.dfd`, the reloader's steps.
- Q10. Metaphor: none, as for the earlier issues.
- Q11. Reviews: the design review and the vocabulary review of the skill at the plan gate, by sub-agents, as the user asked on 2026-10-04 ("use design review by a sub-agent from time to time"); the defect review of AGENTS.md once, before the pull request.
- Q12. Delivery: one pull request after the last stage, with `Closes #34` at creation. The user lifted the halts of law 5 on 2026-10-04 ("no need to ask for approval"), so each boundary records its handoff in the ledger and the work continues.
- Q13. `main.startup` takes a context, which stops the reloader's checks, so that a test's reloader stops with the test. `main` passes `context.Background`.

## The change in brief

The operator points docportal at a bucket folder with `-root`, and docportal reads the folder, its configuration file included, into a snapshot that it serves from memory. Every refresh interval the reloader lists the folder, and a settled change becomes a new snapshot whose portal handler replaces the old one in one step, so that every request reads one snapshot and links between documents lead to one version. The chat keeps its conversations across snapshots. A local directory stays a documentation source that the portal reads live.

## Stages

Each stage ends with `devbox run make build lint test test-e2e`, the hole census (`grep -rn "HOLE(" --include='*.go' .`), the score card, the review page and a ledger line in place of the halt.

The plan gate changes `main.startup` inline: it takes a context, box 2 becomes `main.openSource`, box 8 `source.Load` and box 9 `source.NewReloader`, and the builder closure runs boxes 7, 5, 6 and 3 for each snapshot, with box 6 keeping the chat across builds. The gate declares the package `source` with its types and holes, and `chat.Chat.Reload` as a hole.

### Stage 1: the bucket snapshot

- Goal: docportal serves a bucket folder named by `-root`, read into memory at startup, and a local directory as today.
- Requirement: 1, 2, 3, 7, 8 and 9.
- Dependencies: none.
- Holes: `1 source.Directory.List`, `1 source.Directory.Read`, `1 source.Bucket.List`, `1 source.Bucket.Read`, `1 source.Load`, `1 main.openSource`.
- Acceptance: `TestBucketListing`, `TestBucketSnapshot`, `TestStartupServesABucketFolder`.
- Size: 250 lines.

### Stage 2: the reloader's steps

- Goal: `source.NewReloader` is split into the reloader's steps, drawn in `flow.9.dfd` with holes, and its smoke test passes on their mock data.
- Requirement: 4.
- Dependencies: stage 1.
- Holes: `2 source.NewReloader`, whose body becomes the diagram in code; declares `3 source.reloader.swap`, `3 source.reloader.check`, `3 source.reloader.rebuild` and `3 source.reloader.ServeHTTP`.
- Acceptance: `TestReloaderSmoke`, written in this stage.
- Size: 100 lines.

### Stage 3: the refresh

- Goal: a settled change of the bucket folder replaces the snapshot in service, a failed refresh keeps the old one, and the chat keeps its conversations across snapshots.
- Requirement: 4, 5, 6 and 7.
- Dependencies: stage 2.
- Holes: `3 source.reloader.swap`, `3 source.reloader.check`, `3 source.reloader.rebuild`, `3 source.reloader.ServeHTTP`, `3 chat.Chat.Reload`.
- Acceptance: `TestReloaderSwapsASettledChange`, `TestChatReloadKeepsConversations`, `TestStartupRefreshesTheBucket`, and the browser test `TestRefreshShowsTheNewDocument` in `e2e`.
- Size: 300 lines.
