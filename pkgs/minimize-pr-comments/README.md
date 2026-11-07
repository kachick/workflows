# minimize-pr-comments

Minimize outdated bot comments matching pattern on a pull request.

## Features

- **Match substring markers**:
  Finds comments made by `github-actions[bot]` matching a specific pattern.
- **GraphQL minimization**:
  Marks matching comments as `OUTDATED` using GitHub GraphQL API.
- **Graceful permission fallback**:
  Warns and continues cleanly when the token lacks pull request write permissions.
- **Zero third-party dependencies**:
  Implemented in pure Go standard library.

## Usage

```console
nix run github:kachick/workflows#minimize-pr-comments -- \
  --owner="<owner>" \
  --repo="<repo>" \
  --pr=<pr-number> \
  --marker="<substring>"
```
