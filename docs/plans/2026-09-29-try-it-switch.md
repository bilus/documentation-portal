# Show the Try It console, with a switch that hides it

Issue: bilus/documentation-portal#14 (part of requirement R1, milestone MVP).
Ledger: `docs/plans/2026-09-29-try-it-switch.ledger.md`.
Design: `docs/flow.dfd` and `docs/flow.3.dfd`, terms in `docs/vocabulary.md`, review page in `docs/review/index.html`.
Branch: `bilus/development`, which tracks `origin/master`. Base: `851ea2a`.

## Requirements

Restated from the issue's acceptance criteria and technical notes.

1. Without the switch, the viewer page shows the Try It console of Stoplight Elements. This reverses decision D4 of #2, which hid the console.
2. With `-hide-try-it`, or with `DOCPORTAL_HIDE_TRY_IT` set to a true value, the viewer page hides the Try It console. A value of `DOCPORTAL_HIDE_TRY_IT` that is not a boolean stops startup with an error naming the variable.
3. The portal configuration carries the Try It setting, so an application that embeds the portal sets it too.
4. A browser test shows the console's "Send API Request" button on an operation of a sample bundle by default, and no such button with the setting on.

Out of scope: a proxy for the requests of the Try It console (Elements' `tryItCorsProxy`), and the other options of `elements-api`.

## Questions and assumptions

- Q1. Names: `-hide-try-it` and `DOCPORTAL_HIDE_TRY_IT`, after Elements' own `hideTryIt`. Assumption: those names.
- Q2. Values of the variable. Assumption: `strconv.ParseBool` reads them, so `1`, `t`, `true`, `0`, `f` and `false` work in any case it accepts, and anything else stops startup.
- Q3. Which Elements option. Elements has `hideTryIt`, which hides the console, and `hideTryItPanel`. Assumption: `hideTryIt`, as the viewer page has used since #2.
- Q4. Requests from the console go from the reader's browser to the servers of the API spec, so the API must allow the portal's origin. Assumption: the README says so, and the portal proxies nothing.
- Q5. Design. The Try It setting has to reach the portal configuration. Assumption: a new top box 5, "Add the Try It setting to the portal configuration", between boxes 4 and 3. Widening box 2, or box 4, which already adds a field to the portal configuration, would mix the setting with opening a directory and change an approved signature. An assignment in `main.startup` would make it more than the diagram's calls in order. The cost: a top box whose body is one assignment, one more carried item, and a pattern that a later option of the viewer page would follow with another box or a wider box 5.
- Q6. An existing test changes at this gate: `TestViewerPage` expects `hideTryIt="true"` on the default viewer page. With the console shown by default, that expectation turns into a check that the default page has no `hideTryIt`.
- Q7. Metaphor. Assumption: none, as for #2 and #13.
- Q8. Delivery: one pull request for the whole issue after the last stage's approval, with `Closes #14` in its description at creation.

## The change in brief

The viewer page now shows the Try It console of Stoplight Elements by default. The operator hides it with `-hide-try-it` or a true `DOCPORTAL_HIDE_TRY_IT`, and a value of the variable that is not a boolean stops startup. Startup adds the Try It setting to the portal configuration in a new step after opening the content directory, and the portal passes it to the viewer page, so an application that embeds the portal sets the same field.

## Stages

Each stage ends with `devbox run make build lint test test-e2e`, the hole census (`grep -rn "HOLE(" --include=*.go .`), the score card, the review page and a halt for review.

The skeleton shows the Try It console by default and carries the setting from `portal.Config` to the viewer page, as inline changes with their tests. The one hole is the step that takes the setting from the configuration into the portal configuration.

### Stage 1: the switch

- Goal: `-hide-try-it` and `DOCPORTAL_HIDE_TRY_IT` hide the Try It console of the viewer page, and the README describes the switch and the CORS requirement of the console.
- Requirement: 2.
- Dependencies: none.
- Holes: `1 main.setTryIt`.
- Acceptance: `TestStartupHidesTryIt`.
- Size: 20 lines, below the usual budget, since the issue is small.
