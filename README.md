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

| Reusable Workflow                                     | Description                                                                     |
| :---------------------------------------------------- | ------------------------------------------------------------------------------- |
| [nix-diff-closures](pkgs/nix-diff-closures/README.md) | Post Nix package closure differences on pull requests when `flake.lock` changes |
| [bump-flake-lock](#bump-flake-lock)                   | Update `flake.lock`, run optional post-update command, and create pull request  |

### bump-flake-lock

Update `flake.lock` and create a pull request with GitHub App.

Optionally runs a command after `flake.lock` is updated (for example, to update related CI dependencies) and commits the generated changes before opening the pull request.

#### Usage

```yaml
name: Bump Dependencies

permissions:
  contents: read

on:
  schedule:
    - cron: '17 1 * * 1'
  workflow_dispatch:

jobs:
  bump:
    uses: kachick/workflows/.github/workflows/reusable-bump-flake-lock.yml@main
    with:
      runs-on: ubuntu-26.04
      app-id: ${{ vars.APP_ID }}
      # Optional: run custom package to update related files
      post-commit-lock-file: nix run .#update-deps
      post-commit-lock-file-title: 'Sync CI dependencies with nixpkgs'
    secrets:
      APP_PRIVATE_KEY: ${{ secrets.APP_PRIVATE_KEY }}
```

## Repository Layout

- `.github/workflows/`: Reusable workflows (`workflow_call`) and repository CI
- `pkgs/`: Dedicated CLI tools and documentation for each workflow
