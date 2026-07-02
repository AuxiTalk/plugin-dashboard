# AI Development Guide

This guide is for AI coding agents working on the AuxiTalk Dashboard plugin.

## Responsibilities

- Display plugin status, events, sessions, workflows, and pending actions.
- Provide approve/reject controls for human-in-the-loop operations.
- Keep the dashboard replaceable as a plugin/interface.

## Safe workflow

1. Inspect routes and templates.
2. Preserve server/template conventions.
3. Keep mock data clearly marked until wired to real core data.
4. Run `go test ./...` and project scripts where available.
5. Do not commit logs, PID files, local DBs, or secrets.
6. Commit only when requested.
7. Push only when explicitly requested.

## Sensitive areas

- Rendering secrets from env/config.
- Approval/rejection action semantics.
- Long-running dev server artifacts.
- Real-time event streams.
