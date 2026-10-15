# Development Notes

## Fail-safe over green badges

- Prioritize security above all else. The goal is keeping repositories safe, not keeping CI badges green.
- Merging an uncertain or suspicious change is the worst possible outcome.
- When facing an unknown or unexpected pattern (such as unrecognized pull request titles or ambiguous checks), true fail-safe behavior is to stop: exit with an error, or skip merging and print a log instead of guessing.

## Minimal tooling in nixpkgs

- Add tools only when they are common in nixpkgs `stdenv` or `updateScript`.
- For example, many tools can diff Nix stores, but this repository only uses the official `nix store diff-closures`.

## Restrict external GitHub Actions

- Allow external actions only from `kachick/`, `actions/`, and `NixOS/`.
- Other actions used today are transitional. See https://github.com/kachick/anylang-template/issues/323 for details.

## Action pinning and lockfile

- Actions are currently pinned to commit SHAs.
- Plan to introduce https://github.com/github/gh-actions-lock in the future.

## Bashless and external scripts

- Prefer bashless implementations.
- Keep long bash scripts outside workflow YAML files so `shellcheck` and `dprint` can format and lint them.
- External scripts cannot use GitHub Actions template syntax (`${{ ... }}`), but passing values through environment variables avoids script injection risks.

## Reusable workflow script execution

- Reusable workflows run in the caller repository context, so local scripts cannot be called directly.
- Keep helper scripts inside Nix packages and run them via `nix run` so external repositories can fetch them.

## Language selection

- Any language is acceptable if bashless requirements are met.
- Go is currently chosen to reduce context switching and setup time, and because `go run` is fast and available on standard runners.

## Minimize token scope over immediacy

- Prioritize minimizing the scope and exposure of `github.token` and GitHub App tokens above everything else.
- Be willing to sacrifice immediacy (trading instant event triggers for periodic polling) to avoid unsafe secret access or broad write permissions on pull requests.
- See [`pkgs/automerge`](pkgs/automerge/README.md) for details on this design trade-off.

## Prevent secret leaks to PR comments and external destinations

- GitHub Actions masks registered secrets in console logs, but not in PR comments, titles, or artifacts.
- Scan all text sent to GitHub APIs or external destinations with `betterleaks` before posting.

## Do not add dry-run mode to workflows

- Do not add `dry-run` options to workflows to keep them simple.
- While originally considered for `automerge` and `bump-flake-lock`, dry-run modes cannot test real actions like pushing branches or merging pull requests, and they add unnecessary complexity.

## Workflows write permission trade-off

- Letting Dependabot create Action update PRs avoids write permissions on creation, but merging them still requires `workflows: write`.
- While it can easily become a vulnerability if misconfigured, keeping this permission is an intentional trade-off to automerge Actions updates, not an easy choice to drop.
