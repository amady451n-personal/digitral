# Context for coding agents working in this repo.

## Context management

- **Goal**: avoid autocomplete/autocompact thrashing (context refilled to limit within 3 turns, repeatedly).
- **Primary signal**: any message starting with `<autocompact is thrashing` means reduce context pressure immediately.

## Workflow rules

1. Search before read.
   - `rg <symbol|file>` / `grep` first.
   - `glob` for file patterns.
   - Never read full file unless explicitly required.

2. Read in slices.
   - `Read` tool: use `offset` + `limit` (<=120 lines).
   - Prefer 30-line windows centered on the target line.
   - After compact/autocompact, re-read only the specific range needed.

3. Shell output discipline.
   - Pipe through `head -n`, `tail -n`, or `grep`/`rg` filter.
   - Logs: `rg -i error | head -50`.
   - JSON: `jq -c '.[] | {id}' | head`.
   - Never dump unbuffered full logs/builds.

4. Delegate exploration to subagents.
   - Use `explorer`/`subagent` for repo-wide searches.
   - Main session receives a **summary list + file:line refs**, not full dumps.

5. Compaction cadence.
   - `/compact` only after a finished milestone with a recap of decisions.
   - Avoid `/compact` then immediately reading large files again -> thrashing trigger.
   - On suspected thrashing: `/clear`, recap key facts in 3 bullets, resume.

6. State persistence.
   - Record decisions/diffs in `docs/decisions/` or commit notes.
   - Don't rely solely on chat context for cross-session continuity.

## Safety boundaries

- Never read `node_modules`, `.venv`, or build artifacts fully.
- Never store secrets in this file or scratch paths under version control.
- If a tool output is truncated, ask for a filtered view instead of repeating the full call.

## Language

- Prefer concise, terse responses.
- Keep code paths minimal; prefer stdlib/native over new deps.
