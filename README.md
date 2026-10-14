# workflows

Personal collection of reusable GitHub Actions workflows.

## Assumptions

- **Reusable workflows (`workflow_call`)**:
  Shared jobs that caller repositories can invoke with minimal configuration.
- **Powered by Nix**:
  Back-end CLI tools run directly via `nix run github:kachick/workflows#<target>` on Nix-enabled runners.
  _(Exception: `reusable-betterleaks.yml` is a temporary bridge. It downloads pre-built upstream assets directly to avoid Nix setup overhead on lightweight runners.)_

### This is not the final design

- **Track relaxed revisions**:
  Caller workflows specify `@main` or `@v1` to receive updates without restricted tagging or manual release steps.

## Available Workflows

| Reusable Workflow                                         | Description                                                                     |
| :-------------------------------------------------------- | ------------------------------------------------------------------------------- |
| [nix-diff-closures](pkgs/nix-diff-closures/README.md)     | Post Nix package closure differences on pull requests when `flake.lock` changes |
| [bump-flake-lock](pkgs/bump-flake-lock/README.md)         | Update `flake.lock`, run optional post-update command, and create pull request  |
| [automerge](pkgs/automerge/README.md)                     | Merge eligible bot pull requests on schedule                                    |
| [betterleaks](.github/workflows/reusable-betterleaks.yml) | Scan secrets using upstream binary (temporary non-Nix workflow)                 |

## Repository Layout

- `.github/workflows/`: Reusable workflows (`workflow_call`) and repository CI
- `pkgs/`: Dedicated CLI tools and documentation for each workflow
