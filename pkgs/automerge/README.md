# automerge

Merge eligible bot pull requests on a schedule.

## Background & Motivation

For personal repositories under `kachick/*`, maintaining safe and consistent bot merges requires clear security boundaries and unified logic.

### Primary Goals

1. **Safe and simple token management**:
   Merging with `github.token` blocks downstream workflows (such as CI or deployment) due to GitHub event recursion limits. Passing custom GitHub App secrets into `pull_request` or using `pull_request_target` introduces high security risks. Running scheduled workflows from the default branch keeps private keys safe while creating short-lived App tokens that trigger downstream actions cleanly.
2. **Centralized merge logic and consistency**:
   Putting high-level merge checks (bot filtering, lockfile version inspection, OSV-Scanner diff checks, and LLM final veto) into this shared repository makes bug fixes easy and avoids differences in merge rules across individual repositories. Tailored specifically for personal repositories, this centralization enables high safety standards without per-repository setup toil.

### Byproducts of Scheduled Polling

Accepting a short schedule delay (such as checking every two hours instead of listening to direct `pull_request` webhooks) brings helpful side benefits:

- **Zero waiting overhead**: The job only inspects pull requests where all status checks are already complete. It finishes in seconds without keeping runners busy waiting for other jobs (no need for tools like [`kachick/wait-other-jobs`](https://github.com/kachick/wait-other-jobs)).
- **Zero repository maintenance**: Works out of the box without configuring required status check names in branch protection on every single repository.

## Design Choices & Intentional Limits

- **Why automerge does not approve pull requests**:
  - GitHub forbids approving a pull request with the same account that created it. An app or token cannot approve its own pull requests.
  - Using a separate token or app just to approve pull requests adds secret management complexity and increases security risks.
  - Having a bot approve another bot pull request adds no real security value.
  - If automatic merging is desired on personal repositories, do not require pull request reviews before merging for bot accounts in repository branch protection or rulesets.
- **Why OSV-Scanner diff check runs before LLM review**:
  - Automates deterministic vulnerability checks for modified lockfiles (`go.mod`, `package-lock.json`, `Cargo.lock`, `flake.lock`, etc.) using Google OSV-Scanner.
  - Compares base and PR dependencies to block only newly introduced vulnerabilities. If existing vulnerabilities remain unchanged during an intermediate version bump, the PR safely passes without false positives.
  - Vetoing known vulnerabilities before invoking the LLM completely avoids spending API tokens on vulnerable pull requests.
- **Why automerge posts a comment after merging instead of before**:
  - Without merge queues, an attempt to merge can fail from race conditions or merge conflicts. Posting right after a successful merge ensures comments on pull requests stay accurate.
- **Why automerge labels `automerge:blocked` only for policy blocks (not transient CI failures)**:
  - Transient issues like in-progress CI, temporary test failures, or git conflicts often resolve themselves through subsequent bot updates or retries.
  - Policy blocks (such as major version bumps or group updates) cannot merge without human approval. Labeling these pull requests with `automerge:blocked` allows easy cross-repository filtering (`user:kachick is:pr is:open label:automerge:blocked`) without failing the workflow or creating awkward review rejections.
- **Why automerge writes a Step Summary**:
  - Actions Step Summary provides a clear overview of all inspected pull requests and their results (merged, blocked, vetoed, or skipped) directly in the Actions UI, without digging through runner logs or failing the workflow.
- **Why LLM review acts only as a final veto gatekeeper**:
  - LLM review acts strictly as a negative veto filter right before merging. Gemini inspects the diff and PR description for security risks and breaking changes with severity levels, and code logic decides whether to veto the merge. It never bypasses CI checks or allows ineligible pull requests to merge.
  - When vetoed, the workflow automatically tags the pull request with an `automerge:vetoed` label. Subsequent scheduled runs skip the labeled pull request immediately without spending API tokens. If a human reviewer decides to allow it, simply removing the label will re-enable automatic merging on the next run.
  - When the API key is missing or the API request fails, the merge is safely skipped for the next scheduled run rather than failing the whole workflow.

## Security Guarantees & Invariants

When modifying or extending the automerge workflow, preserve the following invariants:

1. **Negative Veto Only (Never Invert to Positive Allow)**:
   - The review acts strictly as a final **negative veto filter** after all deterministic rules pass (bot verification, draft exclusion, completed green CI checks, and version bump rules).
   - **Critical Warning**: Never invert this flow to an "allow" model (e.g. merging because an LLM approved it, or letting a review bypass failing CI checks). An inverted flow makes prompt injection or hallucinations directly cause unauthorized merges and supply chain attacks.

2. **Negative Labeling for Idempotency (`automerge:vetoed`)**:
   - The label `automerge:vetoed` is strictly a negative filter to skip future runs and save tokens.
   - Because it only denies actions, manipulating the label cannot cause an unauthorized merge:
     - If the label is removed, all deterministic checks and the final veto review run again.
     - If the label is forged or added mistakenly, merging is merely postponed (temporary denial of service, never privilege escalation).
   - **Critical Warning**: Never introduce positive labels (e.g. `automerge:approved`) to authorize merges, because labels can be modified by bots or branch contributors.

3. **Fail-Closed on Review Failure**:
   - If the review API is unavailable, times out, or returns invalid data, the workflow must skip merging for that run. Never fall back to merging unverified pull requests when review is enabled.

4. **Credential Boundaries**:
   - API keys must always be passed via environment variables, not command-line arguments, to prevent leaking secrets in process tables (`ps`).

5. **Input Truncation**:
   - Pull request diffs and descriptions must be truncated to safe length limits to prevent token exhaustion and unexpected API costs.

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

### With LLM Final Veto (Optional)

```yaml
jobs:
  automerge:
    uses: kachick/workflows/.github/workflows/reusable-automerge.yml@main
    with:
      app-id: ${{ vars.SAMU_APP_ID }}
      llm-final-veto: true
    secrets:
      APP_PRIVATE_KEY: ${{ secrets.SAMU_APP_PRIVATE_KEY }}
      GEMINI_API_KEY: ${{ secrets.GEMINI_API_KEY }}
```

### Tips: Distribute Schedules Across Repositories

When using this workflow across multiple repositories under the same owner, avoid using the exact same cron minute (such as `17 */2 * * *`) everywhere.

- **Why**:
  - Repositories share the same GitHub App installation and external API keys (such as `GEMINI_API_KEY`).
  - Running scheduled workflows at the exact same minute concentrates API requests in a short window, which can hit GitHub API rate limits or Gemini API requests-per-minute (RPM) limits.
- **How to pick a distinct minute**:
  Pick a fixed minute (0–59) per repository based on its ID or name:
  ```console
  # Query repository ID and calculate minute (0-59) via GitHub CLI
  gh api repos/:owner/:repo --jq '.id % 60'
  ```
  Or calculate deterministically from the repository name using Ruby:
  ```console
  ruby -e 'require "zlib"; puts Zlib.crc32("kachick/workflows") % 60'
  ```

### CLI

```console
nix run github:kachick/workflows#automerge -- --help
```
