# automerge

Merge eligible bot pull requests.

## Usage

### Reusable Workflow

```yaml
name: Automerge bot PRs

permissions:
  contents: read

on:
  schedule:
    - cron: '17 */2 * * *'
  workflow_dispatch:

jobs:
  automerge:
    uses: kachick/workflows/.github/workflows/reusable-automerge.yml@main
    with:
      app-id: ${{ vars.SAMU_APP_ID }}
    secrets:
      APP_PRIVATE_KEY: ${{ secrets.SAMU_APP_PRIVATE_KEY }}
```

### CLI

```console
nix run github:kachick/workflows#automerge -- --help
```
