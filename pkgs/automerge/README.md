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
- **Why OSV-Scanner diff check runs before LLM review**:
  - Automates deterministic vulnerability checks for modified lockfiles (`go.mod`, `package-lock.json`, `Cargo.lock`, `flake.lock`, etc.) using Google OSV-Scanner.
  - Compares base and PR dependencies to block only newly introduced vulnerabilities. If existing vulnerabilities remain unchanged during an intermediate version bump, the PR safely passes without false positives.
  - Vetoing known vulnerabilities before invoking the LLM completely avoids spending API tokens on vulnerable pull requests.
- **Why LLM review acts only as a final veto gatekeeper**:
  - LLM review acts strictly as a negative veto filter (default: pass) right before merging. It catches malicious code or critical breaking changes without stopping on style or test nitpicks. It never bypasses CI checks or allows ineligible pull requests to merge.
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

### CLI

```console
nix run github:kachick/workflows#automerge -- --help
```
