# Agent Guidelines

## Principles

- Keep items solid and concise (around one line per item) to avoid noise for human maintainers.
- Rules here are preferred guidelines; requirements in `README.md` and `NOTES.md` always take priority.
- If instructions here conflict with requirements in `README.md` or `NOTES.md`, ask the human maintainer before taking action.
- Write only things that humans rarely get wrong but LLMs often miss, adapting to maintainer preferences.

## Documentation Boundaries

- `README.md`: Essential usage and quick references needed during daily operations or onboarding for both humans and agents; keep it solid.
- `pkgs/<name>/README.md`: Workflow-specific details and instructions that are not constantly needed in the root `README.md`.
- `NOTES.md`: Repository-wide topics; separate desired requirements and priorities in upper sections from current choices driven by technical constraints in lower sections.
- `AGENTS.md`: Maintainer preferences and common LLM pitfalls; keep every item concise.
