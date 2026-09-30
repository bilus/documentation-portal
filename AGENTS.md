# Agents

## Reviews

This section supersedes every review instruction of the hole-driven-delivery skill: the vocabulary review and the design review of its step 5, their runs at the plan gate and at stage boundaries, and its `prompts.md`. The rest of the skill still applies, including dfdmetrics, the score card and the review page.

- When: once per issue, on the complete change, after the user approves the last stage and before the pull request opens.
- How: one sub-agent runs the prompt below. Paste its inputs into the prompt, so that it needs few tool calls. It should finish in 5 minutes or less; record its time, tokens and tool calls in the ledger.
- After it reports: run its wrong implementations against the tests with a script, in a scratch copy of the module, and record which of them pass every test. Confirm each high-severity finding with a test or an experiment before acting on it. Record a decision on each finding in the ledger, and plan a new stage for the findings that need changes.

The prompt, with `<base>` the commit before the issue, `<head>` the tip, and `<scratch dir>` a directory in the session's scratchpad:

```
Find defects in a change. Do not edit the repository; report.

Diff: <git diff <base>..<head>, pasted below>
Requirements and decisions: <the plan's Requirements and Questions, pasted below>
Repository: <path>. For experiments, copy it to <scratch dir> and work there.
Budget: at most 10 tool calls, of them at most 4 experiments. When the budget is spent, report.

Look for four kinds of defect, in this order.

1. Bypassed guarantees. List what the change keeps hidden, filtered, contained or validated,
   and every path by which the guarded data can reach a response. Check each path. Look for:
   - a second reader of the same source that skips the filter or the check;
   - indirection that the check does not follow, or follows out of bounds: symlinks, YAML
     aliases, includes, redirects;
   - a check on raw bytes or names that disagrees with the parsed value: escapes,
     percent-encoding, letter case, a second encoding;
   - a boundary that this change moved or merged, such as one root where there were two.
2. Format semantics. For each format the change reads or writes, such as YAML, URLs, JSON
   pointers, markdown or another component's routes, list the features the code can mishandle
   and probe those that apply: several documents, aliases, duplicate keys, a scalar where a
   list belongs, escape sequences and their order, dot segments, empty, host-only or
   unparsable input.
3. Weak tests. Write up to 8 wrong implementations of the changed functions, each a bug a
   careful developer could make, as exact replacements (file, old text, new text) that
   compile. Do not run them; the caller runs them against the tests.
4. Contracts. Compare each changed function's doc comment, and each requirement, with the
   code's behavior on edge inputs. Report each disagreement, including an error the code
   ignores because a caller is assumed to have checked first.

Skip wording, glossary terms, diagram style and names, unless a word misstates behavior.

Return at most 8 findings, most severe first. For each, give the input, what happens, what
the requirements expect, and whether an experiment confirmed it or the code suggests it.
Then list the wrong implementations as a table: name, file, old text, new text.
```
