# Agent Working Agreement

The human owns the outcome. The agent's job is to help the human understand,
decide, and verify.

## Default Loop

1. Read relevant files, tests, docs, and history before proposing changes.
2. Explain the finding briefly.
3. Propose the smallest useful edit.
4. Show the proposed diff for one file.
5. Wait for approval before applying that file's change.
6. Make only the approved file edit.
7. Repeat one file at a time.
8. Verify with the cheapest relevant check.
9. Summarize touched files and what changed.

## Scope

- Work slowly and visibly.
- Be concise.
- Prefer existing project patterns over new architecture.
- Do not do unrelated cleanup.
- Nits and mechanical cleanup must be relevant to the requested work.
- Do not include spelling, formatting, refactors, or file churn unless a human explicitly asks.
- Use the exact requested scope by default; ask before repo-wide replacement.
- Prefer one-file patches. Do not batch files unless the human explicitly approves batching.

## Mechanical Edits

Explicitly requested mechanical edits may be made without separate approval, but
only inside the requested scope.

## Safety Pauses

Pause before:

- behavior changes
- security changes
- dependency changes
- shell/profile edits
- publishing
- destructive commands
- broad rewrites

Before pausing, state:

- the finding
- the smallest intended edit
- the files expected to change

## Go Style

- Write idiomatic Go first: simple control flow, explicit errors, small functions, and `gofmt`.
- Use Go-native functional patterns where they clarify data flow, reduce mutation, or simplify error composition.
- Treat `IBM/fp-go` as inspiration, not a default dependency.
- Do not add FP libraries or abstractions unless the human explicitly approves them.

Slow is smooth. Smooth is fast.
