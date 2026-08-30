# CLAUDE.md — Project Conventions for new-api

## MANDATORY: Read AGENTS.md with the Read tool

Do not treat `@AGENTS.md` as loaded. Claude Code does not reliably inline that import.

Before any planning, coding, reviewing, or answering a project question, you MUST call the Read tool on the repo-root file `AGENTS.md` and wait for the full contents. This is the first action of every session and every new task.

Rules:

- Do not start from memory, summaries, or this file alone.
- Do not skip the Read because a previous turn mentioned AGENTS.md.
- Do not replace the Read with a grep, glob, or partial skim.
- After reading, follow every rule in `AGENTS.md` for the rest of the work.
- If the task touches `web/`, also Read `web/AGENTS.md` before editing frontend files.

## Claude Code

- Follow the shared project instructions imported from `AGENTS.md`.
- **NEVER open a pull request against the upstream repository `https://github.com/QuantumNous/new-api`.** All PRs MUST target this fork (`chunfeng789/new-api`) only — always pass `--repo chunfeng789/new-api` to `gh pr create`, because it otherwise defaults to the upstream parent. See the **Pull requests** rules in `AGENTS.md`.
