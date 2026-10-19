# Development Notes

## Security Principles & Permission Boundaries

### Fail-safe over green badges

- Prioritize security above all else. The goal is keeping repositories safe, not keeping CI badges green.
- Merging an uncertain or suspicious change is the worst possible result.
- When facing an unknown or unexpected pattern (such as unrecognized pull request titles or ambiguous checks), true fail-safe behavior is to stop: exit with an error, or skip merging and print a log instead of guessing.
- Positive approvals (such as GitHub Copilot code review recommendations) can miss subtle mismatches between pull request titles and diffs. For example, in [`kachick/infra-test#1`](https://github.com/kachick/infra-test/pull/1#pullrequestreview-5548352008), Copilot recommended approval for a pull request adding an MIT license despite a title claiming a copyleft license. Negative veto filters (`automerge:vetoed`) remain essential for defense in depth.
- In personal repositories, GitHub charges AI credits for automatic Copilot code review to the pull request author, avoiding wallet drain attacks from spam pull requests. Because bots (such as Renovate) do not have Copilot licenses, automated bot PRs never trigger Copilot reviews. A self-hosted negative veto (`llm-veto`) is therefore the only way to inspect bot updates before merging.

### Minimize token scope over immediacy

- Prioritize minimizing the scope and exposure of `github.token` and GitHub App tokens above everything else.
- Be willing to sacrifice immediacy (trading instant event triggers for periodic polling) to avoid unsafe secret access or broad write permissions on pull requests.
- See [`pkgs/automerge`](pkgs/automerge/README.md) for details on this design trade-off.

### Why not `workflow_run` for automerge and bot sweeps

- While `workflow_run` runs in the trusted default branch context with access to repository secrets, periodic sweeps are preferred for automerge and bot maintenance:
  - **Single-workflow limitation**: `workflow_run` triggers only after a single named workflow completes. When a repository runs multiple CI workflows (such as Go tests and Nix checks), waiting for all checks to finish requires complex API polling.
  - **Race conditions from batch PRs**: Bots like Dependabot or Renovate often open multiple pull requests at once. Merging one PR immediately triggers re-runs and rebase conflicts on others. Periodic polling processes PRs in a stable sequence without churn.
  - **Runner and quota efficiency**: Periodic sweeps batch operations together, avoiding runner overhead and saving LLM API limits (for LLM veto reviews) compared to firing separate runs for every pull request event.

### Workflows write permission trade-off

- Letting Dependabot create Action update PRs avoids write permissions on creation, but merging them still requires `workflows: write`.
- While it can easily become a vulnerability if misconfigured, keeping this permission is an intentional trade-off to automerge Actions updates, not an easy choice to drop.

### Prevent secret leaks to PR comments and external destinations

- GitHub Actions masks registered secrets in console logs, but not in PR comments, titles, or artifacts.
- Scan all text sent to GitHub APIs or external destinations with `betterleaks` before posting.

### Restrict and pin external GitHub Actions

- Allow external actions only from `kachick/`, `actions/`, and `NixOS/`.
- Other actions used today are transitional. See https://github.com/kachick/anylang-template/issues/323 for details.
- Actions are currently pinned to commit SHAs. Plan to introduce https://github.com/github/gh-actions-lock in the future.

### Minimal tooling

- Keep external tool dependencies minimal to reduce supply chain risks.
- Add tools only when they are standard in nixpkgs `stdenv` or common `buildInputs`.
- For example, many third-party tools can diff Nix stores, but this repository only uses the official `nix store diff-closures`.

## Architecture & Implementation Choices

### Bashless CLI tools via `nix run`

- Prefer bashless implementations. External scripts cannot use GitHub Actions template syntax (`${{ ... }}`), and passing values through environment variables or CLI flags avoids script injection risks.
- Keep any remaining bash scripts outside workflow YAML files so `shellcheck` and `dprint` can format and lint them.
- Reusable workflows run in the caller repository context, so local scripts cannot be called directly. Keep tools inside Nix packages and run them via `nix run` so external repositories can fetch them.

### Why Nix: Supply chain security and revision matching

- **Reduce supply chain risks for external tools**:
  - Most logic is built directly in Go, but some external tools like `betterleaks` and `osv-scanner` are still necessary.
  - Instead of trusting upstream GitHub release binaries, using packages built by Hydra through nixpkgs reduces supply chain risks in deployment workflows.
  - This provides an established platform alternative to the current trend of requiring build attestations for each separate tool.
  - However, this approach does not stop all risks. For example, the recent malicious commit to Zod happened directly on the source repository rather than in the package release pipeline.
- **Frictionless revision deployment**:
  - Pre-built CLI binaries work well when tool APIs are stable (like the `gh` command).
  - For reusable workflows under active change, preparing and guaranteeing the exact CLI binary matching the referenced workflow revision (such as `@main` or commit SHA) is difficult.
  - Running tools directly from Flake revisions (`nix run github:kachick/workflows/<rev>#<target>`) solves this without friction, removing the need for manual GitHub releases and asset management.

### Why Go: Rich standard library and zero vendorHash toil

- Any language that meets the bashless requirements is acceptable, but Go is chosen for several key advantages:
  - **Self-contained standard library**: While Rust is often the first choice for modern CLI tools, Go provides a strong standard library. Most workflow logic can be implemented without third-party dependencies.
  - **Zero vendorHash maintenance toil**: In fact, `go.mod` in this repository remains empty. This avoids a major pain point of Nix + Go: updating `vendorHash` every time dependencies change. With zero external packages, package definitions keep `vendorHash = null`, removing maintenance toil and keeping Nix builds simple.
  - **Low context switching and fast runs**: Go reduces setup time, and `go test` and `go run` execute fast on standard runners without extra toolchains.

### Vulnerability detection for flake.lock: Why closure diffs with OSV over existing tools

- While `osv-scanner` checks application lockfiles (`go.mod`, `package-lock.json`, etc.) in `automerge`, `flake.lock` updates cannot be scanned directly. `flake.lock` only stores commit hashes of Nixpkgs, not the resolved dependency packages.
- Scanning the entire Nixpkgs repository is not possible and would flag thousands of unrelated CVEs. In addition, analyzing flake structures inside `automerge` does not work well: runner systems (such as macOS vs Linux) differ across repositories, and target derivations (`devShells`, `packages`, etc.) cannot be guessed centrally.
- Instead, the check runs inside PR CI via `nix-diff-closures`. It extracts newly updated packages from the closure diff and queries the OSV API in a single batch request (`POST /v1/querybatch`).

#### Why not alternative tools

- **Why not `NixOS/nix-security-tracker`**:
  - It is a central web service for the NixOS security team to track CVE links across Nixpkgs channels.
  - It does not provide a CLI or public API to scan user-provided `flake.lock` files or closure store paths on demand.
- **Why not `nix-community/vulnix`**:
  - It matches derivations against NVD (NIST) using rough name rules.
  - Checking whole closures produces many false positives. This creates heavy ignore-list maintenance toil and stops unattended automerge.
  - It also adds Python dependencies and hits strict NVD API rate limits.
- **Why not `anchore/grype` (with Syft)**:
  - Scanning whole closure trees flags false positives for packages where Nixpkgs already backported security fixes.
  - Downloading large vulnerability databases adds runner overhead and external tool dependencies, going against the minimal tooling policy.

#### Advantages of closure diffs with OSV batch query

- **Zero noise from unchanged packages**:
  Checking only packages that changed versions in the diff catches newly introduced vulnerabilities without stopping automerge for existing issues.
- **Minimal tooling and zero vendorHash toil**:
  The OSV batch API accepts package names and versions directly. It runs in pure Go using only the standard library, keeping `vendorHash = null` and adding no external CLI tools.
- **Shared protection across all Nix targets**:
  The same check protects devShell tools in small repositories as well as full system closures in NixOS and Home Manager configurations.
- **Clear CI boundaries**:
  Diff generation and PR comments finish cleanly. A separate gate step fails intentionally when vulnerabilities exist, making the cause clear in the GitHub Actions UI while safely stopping `automerge`.

### Do not add dry-run mode to workflows

- Do not add `dry-run` options to workflows to keep them simple.
- While originally considered for `automerge` and `bump-flake-lock`, dry-run modes cannot test real actions like pushing branches or merging pull requests, and they add unnecessary complexity.
