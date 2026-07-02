# AGENTS.md

This repository is the AuxiTalk Dashboard plugin.

It provides a UI/control surface for observing runtime state, plugins, events, sessions, workflows, and pending actions.

## Required context

Read first:

1. `README.md`
2. `docs/ai-development-guide.md`
3. `dev.sh`
4. `internal/server/*`
5. `internal/templates/*`

## Required checks

Before finishing code changes, run available checks:

```sh
go test ./...
```

If templates are changed, regenerate/build them using the project scripts when available.

## Safety rules

- Do not commit runtime logs, PID files, local databases, or secrets.
- Do not expose secrets in rendered pages.
- Actions shown in UI must preserve approval/rejection semantics.
- Keep mock data clearly separated from real runtime data.

## Product framing

Dashboard is one interface for AuxiTalk. The core must remain usable through CLI, chat, API, and other interfaces too.
