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
   - Vulnerability scans and LLM review act strictly as **negative veto filters** _after_ all deterministic checks pass (allowed bot author, non-draft status, `MERGEABLE` state, completed green CI checks, and allowed version bump).
   - Never invert this flow to a positive "allow" model (such as merging because an LLM approved a diff, or letting an LLM bypass failing CI or major version blocks). In a positive allow model, prompt injection or model hallucinations directly cause unauthorized merges.
   - Keep the LLM strictly as a stateless risk analyzer: do not tell the model its pipeline role (such as "you are the final check" or "CI already passed"), and do not ask it whether to merge. How the model reacts to role pressure or prior green checks is unpredictable; Go code controls the workflow and makes the final veto decision from the reported severity.

2. **Negative State Only (`automerge:vetoed`, `automerge:blocked`)**:
   - Pull request labels are used strictly as negative filters to stop automatic merging.
   - Because labels only deny merges, label tampering cannot escalate privileges:
     - Removing a label simply causes deterministic checks, vulnerability scans, and LLM veto review to run again from scratch.
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
  - With GitHub Copilot code review auto-approval enabled, Copilot reviews diffs and can submit approvals that count toward required approvals in repository rulesets.
  - When requiring Copilot approvals, set `required_approving_review_count` in the target repository ruleset. `automerge` needs no code changes because it relies on GitHub's native mergeable checks and `gh pr merge`.
  - Copilot approvals act as positive approvals, while `automerge` LLM reviews and OSV-Scanner act as negative veto filters. Keeping both provides defense in depth against prompt injection and supply chain attacks.
- **Why OSV-Scanner runs before LLM review**:
  - `osv-scanner` deterministically compares base and PR trees to catch newly introduced CVEs in lockfiles (`flake.lock`, `go.mod`, `Cargo.lock`, `package-lock.json`, etc.).
  - Running this check first rejects known vulnerabilities immediately without spending LLM API tokens.
- **Why negative labels are attached on block or veto**:
  - Tagging `automerge:vetoed` prevents subsequent polling runs from calling the LLM API repeatedly on the same rejected diff. Removing the label re-enables evaluation on the next run.
  - Tagging `automerge:blocked` on policy blocks (such as major updates) allows cross-repository filtering (`user:kachick is:pr is:open label:automerge:blocked`). Transient states like pending CI or merge conflicts are not labeled because they resolve on their own.
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

When running `automerge` across multiple repositories under the same owner, pick a different cron minute per repository so shared GitHub App and Gemini API rate limits are not hit at the same instant:

```console
# Query repository ID and calculate minute (0-59) via GitHub CLI
gh api repos/:owner/:repo --jq '.id % 60'

# Or calculate deterministically from the repository name using Ruby
ruby -e 'require "zlib"; puts Zlib.crc32("kachick/workflows") % 60'
```

### CLI

```console
nix run github:kachick/workflows#automerge -- --help
```
