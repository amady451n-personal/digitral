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

<!-- antislop:start -->
## antislop
For UI, copy, people, mobile layout, or code comments work, read these installed skill files directly (use these paths even if a same-named global skill exists):
- Core filter, always on: `antislop`: `.opencode/skills/antislop/SKILL.md`
- UI / visual: `antislop-ui`: `.opencode/skills/antislop-ui/SKILL.md`
- People: `antislop-human`: `.opencode/skills/antislop-human/SKILL.md`
- Mobile / responsive: `antislop-layoutmobile`: `.opencode/skills/antislop-layoutmobile/SKILL.md`
- Code comments: `antislop-code`: `.opencode/skills/antislop-code/SKILL.md`
Before starting, follow the core's "Two Usage Modes" section in strict order: explicit session instruction first, then global preference, then ask. A session instruction always wins. For a resolved mode, say `antislop active: <mode> (session override).` or `antislop active: <mode> (global preference).` once before presenting findings or making edits, using the actual mode and source. Acknowledging the user's request without naming the source does not replace this notice.
Only an explicit choice of antislop during or after selects a session mode. A request to review, audit, or avoid file edits does not select a mode; read the global preference in that case. Another skill's mode does not select antislop's mode.
If the mode is unresolved, ask during/after and end the response; wait for the answer before any UI review, planning, or concept. For read-only tasks, put the active-mode notice only at the start of the final answer, never in progress messages. For editing tasks, announce before the first edit and omit it from the final answer.
To update antislop later: `npx antislop-ai --update`, or run `npx antislop-ai` and pick Overwrite them.
<!-- antislop:end -->
