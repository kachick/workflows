# automerge

Merge eligible bot pull requests via polling.

## Background & Motivation

Maintaining safe bot merges across `kachick/*` repositories requires strict security boundaries and shared rules.

1. **Safe token isolation via polling**:
   Merging with `github.token` blocks downstream workflows (such as CI or release jobs) due to GitHub event recursion limits. Passing GitHub App secrets into `pull_request` or using `pull_request_target` exposes credentials to untrusted code. Running scheduled polling workflows from the default branch keeps private keys out of pull request contexts while minting short-lived App tokens that trigger downstream jobs cleanly.
2. **Centralized merge policy**:
   Keeping bot allowlists, version bump rules, differential vulnerability scans, and LLM veto logic in one repository ensures consistent safety standards without per-repository setup toil.

## Security Requirements & Invariants

Preserve the following invariants when modifying or extending `automerge`:

1. **Negative Veto Only (Never Positive Allow)**:
   - Vulnerability scans and LLM veto act strictly as **negative veto filters** _after_ all deterministic checks pass (allowed bot author, non-draft status, `MERGEABLE` state, completed green CI checks, and allowed version bump).
   - Never invert this flow to a positive "allow" model (such as merging because an LLM approved a diff, or letting an LLM bypass failing CI or major version blocks). In a positive allow model, prompt injection or model hallucinations directly cause unauthorized merges.
   - Keep the LLM strictly as a stateless risk analyzer: do not tell the model its pipeline role (such as "you are the final check" or "CI already passed"), and do not ask it whether to merge. How the model reacts to role pressure or prior green checks is unpredictable; Go code controls the workflow and makes the final veto decision from the reported severity.

2. **Negative State Only (`automerge:vetoed`, `automerge:blocked`)**:
   - Pull request labels are used strictly as negative filters to stop automatic merging.
   - Because labels only deny merges, label tampering cannot escalate privileges:
     - Removing a label simply causes deterministic checks, vulnerability scans, and LLM veto to run again from scratch.
     - Adding a label only postpones merging.
   - Never introduce positive labels (such as `automerge:approved`) to authorize merges, because labels can be modified by bots or collaborators.

3. **Fail-Closed on Uncertainty or Error**:
   - Any unrecognized PR title, unknown Renovate table row, diff fetch failure, scanner error, missing API key, or LLM timeout must skip or block the merge. Never fall back to merging unverified changes.

4. **Trusted Baseline & Credential Isolation**:
   - Differential vulnerability scans (`osv-scanner`) must always use configuration (`osv-scanner.toml`) from the trusted base branch, never from the patched PR tree.
   - Pass API keys via environment variables and HTTP headers (`x-goog-api-key`), never via CLI flags or URL query parameters where process tables (`ps`) or network error logs could expose them.

## Implementation Limits & Operational Choices

- **PR review approvals and Copilot code review**:
  - GitHub blocks an account or App from approving its own pull requests. Managing a second bot account just to click approve adds secret management toil without real security value.
  - With [Copilot code review auto-approval](https://github.blog/changelog/2026-09-01-copilot-code-review-can-now-approve-pull-requests/) enabled, Copilot reviews diffs and can submit approvals that count toward required approvals in repository rulesets.
  - Copilot reviews default to comments only. Submitting real approvals (`APPROVED` state) requires turning on "Allow Copilot to approve pull requests" in repository or organization settings.
  - Copilot approvals act as positive approvals, while `automerge` LLM veto and OSV-Scanner act as negative veto filters. Keeping both provides defense in depth:
    - In a real test ([`kachick/infra-test#1`](https://github.com/kachick/infra-test/pull/1#pullrequestreview-5548352008)), a pull request titled "Add Copyleft license" added an MIT license instead. Copilot noted the difference in its summary table but still recommended approval without flagging a problem.
    - Relying only on positive approvals allows subtle mismatches or prompt injections to pass through. Keeping negative veto filters (`automerge:vetoed`) remains essential.
  - **Billing protection and bot PR limits in personal repositories**:
    - GitHub charges AI credit consumption to the pull request author, not the repository owner. This protects public repositories from billing drains by spam pull requests from forks.
    - Because third-party bots (such as Renovate or Dependabot) do not hold personal Copilot licenses, automatic reviews do not run for bot pull requests in personal repositories.
    - Therefore, personal repositories cannot rely on Copilot code review for automated bot merges. Running custom LLM veto (`llm-veto`) with controlled API keys remains necessary to inspect bot pull requests.
  - **Trap: Do not require approvals (`required_approving_review_count >= 1`) in personal repositories**:
    - Because bot pull requests never receive Copilot reviews, requiring approvals will permanently block unattended merges for Dependabot and Renovate.
    - Keep `required_approving_review_count = 0` in personal repository rulesets to preserve unattended bot automerge.
- **Why OSV-Scanner runs before LLM veto**:
  - `osv-scanner` deterministically compares base and PR trees to catch newly introduced CVEs in supported lockfiles (`go.mod`, `Cargo.lock`, `package-lock.json`, etc.). Note that `flake.lock` is not directly supported by `osv-scanner` and is inspected by `nix-diff-closures` instead.
  - Running this check first rejects known vulnerabilities immediately without spending LLM API tokens.
- **Why negative labels are attached on block or veto**:
  - Tagging `automerge:vetoed` prevents subsequent polling runs from calling the LLM API repeatedly on the same rejected diff. Removing the label re-enables evaluation on the next run after updating the review bot or pushing new commits.
  - Tagging `automerge:blocked` on policy blocks (such as major updates) allows cross-repository filtering. Transient states like pending CI or merge conflicts are not labeled because they resolve on their own.
  - Quick filter links across repositories:
    - [Vetoed pull requests (`label:automerge:vetoed`)](https://github.com/pulls?q=is%3Apr+is%3Aopen+user%3Akachick+label%3Aautomerge%3Avetoed)
    - [Blocked pull requests (`label:automerge:blocked`)](https://github.com/pulls?q=is%3Apr+is%3Aopen+user%3Akachick+label%3Aautomerge%3Ablocked)
    - [All pull requests requiring review](https://github.com/pulls?q=is%3Apr+is%3Aopen+user%3Akachick+label%3Aautomerge%3Avetoed%2Cautomerge%3Ablocked)
- **Why LLM inputs are bounded**:
  - Pull request descriptions and diffs are reordered and truncated before calling the LLM API to control token usage and keep model focus on high-signal changes.
- **Side benefits of polling**:
  - Because the job only inspects pull requests whose checks have already completed, it finishes quickly without holding runners open (no need for [`kachick/wait-other-jobs`](https://github.com/kachick/wait-other-jobs)) and requires no per-repository branch protection check lists.

## Usage

### Reusable Workflow

```yaml
name: Automerge bot PRs

permissions:
  contents: read

on:
  schedule:
    # Pick a distinct minute per repository to spread out API requests
    # e.g. run `gh api repos/:owner/:repo --jq '.id % 60'`
    - cron: '17 */6 * * *'
  workflow_dispatch:

jobs:
  automerge:
    uses: kachick/workflows/.github/workflows/reusable-automerge.yml@main
    with:
      app-id: ${{ vars.SAMU_APP_ID }}
    secrets:
      APP_PRIVATE_KEY: ${{ secrets.SAMU_APP_PRIVATE_KEY }}
```

### With LLM Veto (Optional)

```yaml
jobs:
  automerge:
    uses: kachick/workflows/.github/workflows/reusable-automerge.yml@main
    with:
      app-id: ${{ vars.SAMU_APP_ID }}
      llm-veto: true
    secrets:
      APP_PRIVATE_KEY: ${{ secrets.SAMU_APP_PRIVATE_KEY }}
      GEMINI_API_KEY: ${{ secrets.GEMINI_API_KEY }}
```

### Tips: Distribute Schedules Across Repositories

See [Distribute Schedules Across Repositories](../../README.md#distribute-schedules-across-repositories) in the root README for recommended schedule rules and helper scripts.

### CLI

```console
nix run github:kachick/workflows#automerge -- --help
```
