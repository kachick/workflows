# bump-flake-lock

Update `flake.lock` and create a pull request with GitHub App.

Optionally runs a command after `flake.lock` is updated (for example, to update related CI dependencies) and commits the generated changes before opening the pull request.

## Usage

### Reusable Workflow

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

### CLI

```console
nix run github:kachick/workflows#bump-flake-lock -- --help
```
