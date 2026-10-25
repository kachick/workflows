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
- **Differential vulnerability scanning**:
  Compares packages before and after closure updates against the OSV (Open Source Vulnerabilities) database in a single batch query. When newly introduced vulnerabilities are found, the PR comment includes a caution alert and the workflow fails intentionally to prevent unintended automerge. Resolved vulnerabilities are highlighted to help evaluate trade-offs, and pre-existing vulnerabilities are summarized without cluttering the summary.
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

### Tips: External API Limits in Downstream Jobs

`nix-diff-closures` queries Google's OSV API when pull requests change `flake.lock`. To avoid hitting the OSV API all at once across repositories, distribute upstream `bump-flake-lock` schedules.

See [Distribute Schedules Across Repositories](../../README.md#distribute-schedules-across-repositories) in the root README for recommended schedule rules and helper scripts.

### CLI

```console
nix run github:kachick/workflows#nix-diff-closures -- --help
```

## Architecture Notes: Derivations vs. Runtime Closures

`nix-diff-closures` inspects derivation graphs (`--derivation`) instead of realized runtime closures (`/nix/store/<hash>-...`).

### Why Derivations Today (Fast and Evaluation-Only)

- **Speed**: Derivations require only Nix expression evaluation. They finish in seconds without building anything.
- **Runtime closures require building**: In Nix, runtime dependencies are determined only after scanning built files for store paths. Comparing runtime closures requires running `nix build` on both base and head revisions, which easily times out or exhausts disk in CI for full NixOS systems or large packages.

### Ideal Differences by Target Type

- **`devShells`**:
  Derivation-level diffs are ideal. A devshell does not produce a deployed runtime artifact; its whole purpose is providing build-time and developer tools (`gcc`, `bash`, linters). Catching updates to those tools is the desired behavior.
- **`nixosConfigurations` and `homeConfigurations`**:
  Runtime closures are ideal. Transient bootstrap tools and build helpers do not remain on the deployed machine. Vulnerability gates and closure diffs should ideally monitor only the software that actually runs on the system.

### Future Possibility: Post-Build Runtime Diffs

For repositories that already build configurations in CI (such as `dotfiles` building NixOS and home-manager):

- Inserting closure diffs after the build step would allow diffing exact runtime out paths.
- Comparing against the base revision requires realizing the base configuration as well (or fetching it from a binary cache such as Cachix), potentially doubling build work.
- When restricted to periodic update workflows (such as weekly `bump-flake-lock`), this trade-off may be worthwhile to eliminate build-time false alarms.
