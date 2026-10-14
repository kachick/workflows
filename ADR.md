# Architecture Decision Records

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

## Language selection

- Any language is acceptable if bashless requirements are met.
- Go is currently chosen to reduce context switching and setup time, and because `go run` is fast and available on standard runners.
