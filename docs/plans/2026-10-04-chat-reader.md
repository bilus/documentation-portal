# Chat reader: one question limit for each signed-in reader

Issue: bilus/documentation-portal#26.
Ledger: `docs/plans/2026-10-04-chat-reader.ledger.md`.
Design: `docs/flow.dfd`, `docs/flow.3.dfd`, `docs/flow.3.4.dfd` and `docs/flow.9.dfd`; terms in `docs/vocabulary.md`; review page in `docs/review/index.html`.
Branch: a worktree branch, rebased onto `origin/master`. Base: `acd5124`, issue 35 as master holds it; the plan began on `d753641` (ledger).

## Requirements

Restated from issue 26's acceptance criteria, with requirements 3 and 7 from the working assumptions of its first open question and of Q6.

1. `chat.Config` takes the reader hook, `Reader func(r *http.Request) string`, which returns the reader ID of a request's reader, or an empty string for a reader who is not signed in.
2. With a reader hook, the questions of a signed-in reader count against one question limit, whichever client each of the reader's chat tabs came from, and two signed-in readers behind one client each have a question limit of their own.
3. The questions of a reader without a reader ID count against the question limit of the tab's client, and without a reader hook every tab's questions do, as today.
4. A reader ID never shares a question limit with a client, even when its text is the client's, whether a chat tab or a program's own call of `Chat.Ask` asks the question.
5. The chat's `Limits` still set the question limit and its window: `Questions` in `Window`, for readers and clients alike.
6. A chat tab keeps the reader ID of its GET, so its questions after the join, which has no request, count against that reader's question limit.
7. With a reader hook, the chat page answers with `Cache-Control: private`, since the page holds its reader's ID.
8. The README documents the reader hook for an embedding program, and its chat section says how the question limit counts.

The tests drive the chat page through a portal handler and the page's mount, and a browser test loads one reader's chat page from two clients.

Out of scope: a limit on all questions together (the issue's second open question), sign-in (#8), and the documentation of the request hooks together (#9).

## Questions and assumptions

- Q1. The hook. Assumption: `chat.Config.Reader func(r *http.Request) string`, which returns an empty string for a reader who is not signed in. It has no error result, unlike the access hook: it reads what the program's middleware put into the request's context, and a reader whom it cannot name counts against the limit of the client, so every question stays under a limit. An error of the access hook has to hide everything, which needs the error.
- Q2. Sign-in for the chat (the issue's first open question). Assumption: the chat keeps the client's limit for readers who do not sign in. A program that wants every chat reader signed in requires it in its middleware or its access hook.
- Q3. A limit on all questions together (the issue's second open question). Assumption: not in this change; it waits for an issue of its own. The limit of each reader ID bounds a signed-in reader's cost, and the limit of each client the rest, as today.
- Q4. The asker. Assumption: the question limit counts each question against an asker, the type `chat.Asker`, which `chat.ReaderAsker(id)` makes for a signed-in reader and `chat.ClientAsker(client)` for any other reader, each in a namespace of its own, so that a reader ID never names a client's limit (requirement 4). A chat tab asks as `askerOf` its page session gives: the reader of its reader ID, else its client. `Chat.Ask` takes the asker in place of the client string, so that a program that calls it for a reader shares the limit of that reader's chat pages; the design review at the gate found that a string, as the plan first had it, gave one reader two limits (ledger).
- Q5. The socket. Assumption: as the page access of #35 (its Q8). The chat's `sessionOf` asks the reader hook at the page's GET, which passes through the portal handler, and live-templ signs the reader ID into the page session beside the client; the page's mount at the GET and at the join reads both from there. A reader cannot change the ID, since live-templ signs the page session, but the reader's browser can read it, since signing does not encrypt (live-templ's `WithSession`), so the README asks for an ID that the reader may see, such as the subject of the reader's token.
- Q6. Cache-Control (requirement 7). Assumption: with a reader hook, the chat sets `Cache-Control: private` on each response of its pages before live-templ's handler runs, as the portal handler's private writer does with an access hook (#35, Q7); without one, the page's responses stay as they are. A shared cache could otherwise give one reader's page, with that reader's ID, to another reader, whose questions would then count against the first reader's limit.
- Q7. Design. The chat's runtime has no diagram of its own, as since #25 and in #35 (its Q10), but its state and I/O behind the routes of box 3.4.3 are drawn there, as the gate's design review asked: the chat page's GET asks the embedding program for the reader ID and, through the page access, about each portal and section; each question over the chat's socket, which lives in the route of its upgrade request, reads and writes the counts of the question limit, the private store item `questions by asker` of `|Question limit|`, and asks `<Chat model>`; chat problems go to `<Log>`. No box or flow arrow changes, and `flow.dfd` stays as it is, since docportal sets no reader hook. The page session's round trip through the browser stays undrawn.
- Q8. Metaphor: none, as for the earlier issues.
- Q9. Reviews: the design review and the vocabulary review of the skill at the plan gate, by sub-agents, as the user asked on 2026-10-04 ("use design review by a sub-agent from time to time"); the defect review of AGENTS.md once, after the last stage.
- Q10. Delivery: one commit per filled hole, no pull request and no push; the main session reshapes the history and opens the pull request. The user lifted the halts of law 5 ("no need to ask for approval"), so each boundary records its handoff in the ledger and the work continues.

## The change in brief

An embedding program gives `chat.Config` a reader hook, a request hook that names the reader ID of each request's reader, such as from the claims of the program's own middleware. At a chat page's GET, the chat asks the hook and keeps the reader ID in the page session beside the client, and the page's mount, at the GET and again at the join, makes the chat tab's asker from them: the reader ID of a signed-in reader, else the client. The question limit counts each question against its asker, a `chat.Asker`, so a signed-in reader's questions share one question limit from every client, and every other reader's count by client, as without a hook; a program that calls `Chat.Ask` itself asks as `ReaderAsker` or `ClientAsker`. With a reader hook, the chat page's responses are private.

## Stages

Each stage ends with `devbox run make build lint test test-e2e`, the hole census (`grep -rn "HOLE(" --include='*.go' .`), the score card, the review page and a ledger line in place of the halt.

The plan gate declares the reader hook, `Config.Reader`, kept by `New`, the type `Asker`, and five holes in a new file `chat/reader.go`: `ReaderAsker`, `ClientAsker`, `Chat.readerOf`, `askerOf` and `Chat.privatePage`, whose mocks keep today's behavior: an asker named by its text alone, no reader ID, the client as the page's asker, and the page's responses as they are. Inline, the session reader puts the reader ID into the page session, the mount takes the page's asker from `askerOf`, `Routes` wraps each chat page in `privatePage`, and `Ask`, `admit` and the question counts take an `Asker` in place of the client string; a script changes the tests' calls of `Ask` and `admit` to ask as `ClientAsker` of their client. It writes the acceptance tests, skipped with their tags.

### Stage 1: the question limit of each reader

- Goal: a signed-in reader's questions count against one question limit from any address, a reader without a reader ID and every reader without a hook count by client, the chat page is private with a reader hook, and the README documents the hook.
- Requirement: 1 to 8.
- Dependencies: none.
- Holes: `1 chat.ReaderAsker`, `1 chat.ClientAsker`, `1 chat.Chat.readerOf`, `1 chat.askerOf`, `1 chat.Chat.privatePage`.
- Acceptance: `TestOneLimitForAReaderFromAnyClient`, `TestAReaderIDNeverSharesAClientsLimit`, `TestAskCountsAReaderApartFromAClient`, `TestTheChatPageIsPrivateWithAReaderHook`, and the browser test `TestOneQuestionLimitForAReaderInTheBrowser` in `e2e`. `TestPagesWithoutAReaderIDCountByClient` passes on the mocks, as a guard of today's behavior.
- Size: 150 lines, with the README.
