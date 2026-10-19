# workflows

Personal collection of reusable GitHub Actions workflows.

## Assumptions

- **Reusable workflows (`workflow_call`)**:
  Shared jobs that caller repositories can invoke with minimal configuration.
- **Powered by Nix**:
  Back-end CLI tools run directly via `nix run github:kachick/workflows#<target>` on Nix-enabled runners.

### This is not the final design

- **Track relaxed revisions**:
  Caller workflows specify `@main` or `@v1` to receive updates without restricted tagging or manual release steps.

## Available Workflows

| Reusable Workflow                                         | Description                                                                     |
| :-------------------------------------------------------- | ------------------------------------------------------------------------------- |
| [nix-diff-closures](pkgs/nix-diff-closures/README.md)     | Post Nix package closure differences on pull requests when `flake.lock` changes |
| [bump-flake-lock](pkgs/bump-flake-lock/README.md)         | Update `flake.lock`, run optional post-update command, and create pull request  |
| [automerge](pkgs/automerge/README.md)                     | Merge eligible bot pull requests via polling                                    |
| [betterleaks](.github/workflows/reusable-betterleaks.yml) | Scan secrets using upstream binary (temporary non-Nix workflow)                 |

## Triage & PR Review

When `automerge` skips a pull request due to risk detection or policy blocks, it attaches a negative label for human inspection. Filter these pull requests across repositories:

- [All pull requests requiring human review](https://github.com/pulls?q=is%3Apr+is%3Aopen+user%3Akachick+label%3Aautomerge%3Avetoed%2Cautomerge%3Ablocked): Combined view of vetoed and blocked pull requests

## Repository Layout

- `.github/workflows/`: Reusable workflows (`workflow_call`) and repository CI
- `pkgs/`: Dedicated CLI tools and documentation for each workflow
