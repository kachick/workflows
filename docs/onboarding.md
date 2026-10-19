# Onboarding

Guide for onboarding and installing workflows in caller repositories.

## Available Workflows

| Reusable Workflow                                            | Description                                                                     |
| :----------------------------------------------------------- | ------------------------------------------------------------------------------- |
| [nix-diff-closures](../pkgs/nix-diff-closures/README.md)     | Post Nix package closure differences on pull requests when `flake.lock` changes |
| [bump-flake-lock](../pkgs/bump-flake-lock/README.md)         | Update `flake.lock`, run optional post-update command, and create pull request  |
| [automerge](../pkgs/automerge/README.md)                     | Merge eligible bot pull requests via polling                                    |
| [betterleaks](../.github/workflows/reusable-betterleaks.yml) | Scan secrets using upstream binary (temporary non-Nix workflow)                 |

## Assumptions

- **Reusable workflows (`workflow_call`)**:
  Shared jobs that caller repositories can invoke with minimal configuration.
- **Powered by Nix**:
  Back-end CLI tools run directly via `nix run github:kachick/workflows#<target>` on Nix-enabled runners.

### This is not the final design

- **Track relaxed revisions**:
  Caller workflows specify `@main` or `@v1` to receive updates without restricted tagging or manual release steps.

## Distribute Schedules Across Repositories

When managing multiple repositories under the same owner, distribute cron schedules to avoid hitting rate limits on external APIs and runner contention.

### Downstream API Chain

The workflows connect in an automated chain:

1. `bump-flake-lock` opens a monthly dependency update pull request.
2. The pull request triggers `nix-diff-closures` in CI, which queries **Google's OSV API** (`api.osv.dev`).
3. Scheduled `automerge` inspects eligible bot pull requests and queries **Google's Gemini API** for LLM veto review.

If all repositories run at the exact same minute, shared GitHub App limits and downstream APIs (OSV and Gemini) get hit all at once.

### Recommended Schedule Rules

- **`bump-flake-lock` (Monthly on the 1st)**:
  - **Keep the day fixed (1st of month)**: Sharing the same Nixpkgs revision across projects keeps local Nix store size small and saves rebuild time.
  - **Distribute hour (0-5) and minute (0-59)**: Spreads out pull request creation so downstream `nix-diff-closures` does not hit the OSV API all at once.
- **`automerge` (Polling every 6 hours)**:
  - **Distribute minute (0-59)**: Spreads out polling runs so Gemini API and GitHub App limits are not hit in the same minute.

### Deterministic Schedule Helper (Ruby)

```console
# For bump-flake-lock: 1st of month with distributed hour and minute
ruby -e '
  require "zlib"
  crc = Zlib.crc32("kachick/workflows")
  puts "#{crc % 60} #{(crc / 60) % 6} 1 * *"
'

# For automerge: every 6 hours with distributed minute
ruby -e '
  require "zlib"
  crc = Zlib.crc32("kachick/workflows")
  puts "#{crc % 60} */6 * * *"
'
```

## Repository Layout

- `.github/workflows/`: Reusable workflows (`workflow_call`) and repository CI
- `pkgs/`: Dedicated CLI tools and documentation for each workflow
