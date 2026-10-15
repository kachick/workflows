# nix-diff-closures

Post Nix closure differences on pull requests when `flake.lock` changes.

## Features and Non-Obvious Requirements

- **Multiple targets in a single run**:
  Supports checking multiple targets (such as `devShells`, `packages`, `nixosConfigurations`, and `homeConfigurations`) at once and groups them into one clean pull request comment.
- **Minimal dependencies**:
  Uses only the official `nix store diff-closures` command from Nix. Does not require external diff tools (such as `nvd` or `nix-diff`).
- **Safe comment limit fallback**:
  When a diff becomes very large (for example, after upgrading a stable Nixpkgs channel), the comment is safely shortened with a link to job artifacts. This avoids GitHub API size limits.
- **UTF-8 safe text handling**:
  Handles non-ASCII characters from Nix diffs (such as `∅`, `→`, and `ε`) safely without cutting multi-byte runes, avoiding broken characters in comments.
- **Auto-minimize older comments**:
  Minimizes previous diff comments as `OUTDATED` when new commits are pushed, keeping pull request discussions clean without duplicate noise.
- **Reusable workflow**:
  Provided as a reusable workflow (`workflow_call`) so other repositories can use it with only a few lines of configuration.
- **Logic inside a dedicated CLI**:
  Keeps shell scripts minimal and puts core logic (diffing, truncation, and GitHub API operations) into a Go CLI to make local testing easy.

## Usage

### Reusable Workflow

```yaml
name: ❄️ Diff
on:
  pull_request:
    paths:
      - 'flake.lock'

permissions:
  contents: read
  pull-requests: write

jobs:
  diff:
    uses: kachick/workflows/.github/workflows/reusable-nix-diff-closures.yml@main
    with:
      runs-on: ubuntu-26.04
      targets: |
        devShells.x86_64-linux.default
        nixosConfigurations.desktop.config.system.build.toplevel
        homeConfigurations.kachick.activationPackage
```

### CLI

```console
nix run github:kachick/workflows#nix-diff-closures -- --help
```
