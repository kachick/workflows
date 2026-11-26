# cleanup-ghcr

Prune old GHCR container package versions or remove temporary PR staging versions.

## Flow

```mermaid
flowchart TD
  PruneWF["reusable-cleanup-ghcr.yml (packages: write)"]
  StagingWF["reusable-cleanup-ghcr-staging.yml"]
  MinJob["Job: minimize-old-comments (if body-includes set, pull-requests: write)"]
  MinCLI["minimize-pr-comments"]
  PruneCLI["cleanup-ghcr --mode=prune"]
  StagingCLI["cleanup-ghcr --mode=staging (packages: write, pull-requests: write)"]
  List["List GHCR container versions (GitHub REST API)"]
  SelectPrune["Keep newest N versions, delete older (up to max-delete)"]
  SelectStaging["Find versions tagged pr-<number>-* and delete them"]
  Comment["leakcheck + post deleted version IDs to PR comment"]

  PruneWF --> PruneCLI --> List
  StagingWF --> MinJob --> MinCLI
  StagingWF --> StagingCLI --> List
  List -->|"prune mode"| SelectPrune
  List -->|"staging mode"| SelectStaging --> Comment
```

## Usage

### Prune Old Versions (`reusable-cleanup-ghcr.yml`)

```yaml
jobs:
  cleanup:
    uses: kachick/workflows/.github/workflows/reusable-cleanup-ghcr.yml@main
    with:
      package-name: 'home'
      keep-latest-items: 50
```

### Remove PR Staging Versions (`reusable-cleanup-ghcr-staging.yml`)

```yaml
jobs:
  cleanup-staging:
    uses: kachick/workflows/.github/workflows/reusable-cleanup-ghcr-staging.yml@main
    with:
      package-name: 'home'
      pr-number: ${{ github.event.pull_request.number }}
      body-includes: 'staging-deploy-comment' # optional
```

### CLI

```console
nix run github:kachick/workflows#cleanup-ghcr -- --help
```
