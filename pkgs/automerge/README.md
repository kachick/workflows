# automerge

Merge eligible bot pull requests on a schedule.

## Background & Motivation

Triggering automerge directly on `pull_request` events seems natural, but it creates hard dilemmas in GitHub Actions:

- **`github.token` limitations**:
  Merging with `github.token` requires write permissions, but GitHub prevents recursion, so merged commits on `main` will not trigger downstream workflows (such as CI or deployment).
- **GitHub App token and secret boundaries**:
  Using a GitHub App token solves the recursion issue, but creating it requires private keys stored in repository secrets. Automated bot pull requests cannot access secrets on `pull_request`, and using `pull_request_target` introduces severe security risks. Setting up and managing custom secrets or apps for each bot across multiple repositories is fragile and tedious.
- **Runner time waste from waiting for status checks**:
  A merge workflow must wait for all other status checks to finish before merging. Waiting inside actions (for example, with tools like [`kachick/wait-other-jobs`](https://github.com/kachick/wait-other-jobs)) keeps runners busy and wastes runner minutes.
- **Maintenance cost of native auto-merge**:
  GitHub native platform auto-merge requires enabling auto-merge and configuring exact required status check names in branch protection on every single repository.

### The Solution: Scheduled Polling

Accepting a small delay (such as running every two hours instead of immediate event triggers) cleanly resolves all these problems:

1. **Safe secret access**: Scheduled workflows run in the default branch context and can safely access repository secrets to create a GitHub App token.
2. **Proper event chaining**: Merges made with the GitHub App token reliably trigger `main` branch workflows.
3. **Zero waiting overhead**: The job only inspects pull requests where all status checks are already completed, finishing in seconds without keeping runners waiting.
4. **Zero repository maintenance**: Works out of the box without configuring required status check names on each repository.

## Design Choices & Intentional Limits

- **Why automerge does not approve pull requests**:
  - GitHub forbids approving a pull request with the same account that created it. An app or token cannot approve its own pull requests.
  - Using a separate token or app just to approve pull requests adds secret management complexity and increases security risks.
  - Having a bot approve another bot pull request adds no real security value.
  - If automatic merging is desired on personal repositories, do not require pull request reviews before merging for bot accounts in repository branch protection or rulesets.
- **Why `runs-on` is not an input**:
  - Automerge is a chore workflow with fixed runner needs. It runs on `ubuntu-26.04` directly without matrix setups or different platforms.

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
