# Architecture

Overview of how caller repositories, reusable workflows, Nix packages, and Go code fit together.
See [NOTES.md](../NOTES.md) for the security principles and trade-offs behind these choices.

## Caller to Runner Execution Model

Caller repositories invoke reusable workflows via `workflow_call`.
Each workflow resolves its own commit SHA (`job.workflow_repository` and `job.workflow_sha`) and runs the matching Go CLI via `nix run`, except `reusable-betterleaks.yml` which downloads the upstream binary directly.

```mermaid
flowchart LR
  Caller["Caller repo (.github/workflows/*.yml)"]
  Reusable["Reusable workflow (.github/workflows/reusable-*.yml)"]
  Flake["flake.nix (auto-discovers pkgs/*/package.nix)"]
  GoCLI["Go CLI (pkgs/<name>/, Go stdlib, vendorHash = null)"]
  Cachix["Cachix binary cache (kachick.cachix.org)"]
  Betterleaks["reusable-betterleaks.yml (no Nix, upstream release + SHA-256)"]

  Caller -->|"workflow_call (@main)"| Reusable
  Reusable -->|"nix run github:<repo>/<sha>#<pkg>"| Flake
  Flake -->|"buildGo127Module"| GoCLI
  Cachix -.->|"substituter"| Flake
  Caller -->|"workflow_call (exception)"| Betterleaks
```

## Workflow Chain and Trust Boundaries

Secret-bearing jobs (`bump-flake-lock`, `automerge`) run on schedules from the default branch and mint short-lived GitHub App tokens.
Pull request jobs (`nix-diff-closures`, `betterleaks`, `cleanup-ghcr-staging`, `minimize-pr-comments`) use only `github.token` with minimal permissions.

```mermaid
flowchart TD
  subgraph Scheduled["Default branch (schedule / workflow_dispatch, GitHub App token)"]
    Bump["bump-flake-lock"]
    Automerge["automerge (polling every 6h)"]
  end

  subgraph PRContext["Pull request context (github.token only)"]
    NixDiff["nix-diff-closures"]
    LeakScan["betterleaks (fetch-depth: 0)"]
    Minimize["minimize-pr-comments"]
    CleanupStaging["cleanup-ghcr-staging"]
  end

  subgraph External["External services and APIs"]
    GitHub["GitHub REST / GraphQL / GHCR"]
    OSV["Google OSV API (api.osv.dev)"]
    Gemini["Google Gemini API (optional LLM veto)"]
  end

  Bump -->|"create or update PR"| GitHub
  GitHub -->|"pull_request event"| NixDiff
  GitHub -->|"pull_request / push event"| LeakScan
  NixDiff -->|"POST /v1/querybatch"| OSV
  NixDiff -->|"minimize old + post diff comment"| GitHub
  Automerge -->|"list open PRs, diff, review, merge"| GitHub
  Automerge -->|"optional LLM veto"| Gemini
  GitHub -->|"PR closed / comment update"| CleanupStaging
  GitHub -->|"comment update"| Minimize
  CleanupStaging -->|"delete staging tags + comment"| GitHub
  Minimize -->|"GraphQL OUTDATED"| GitHub
```

## Go Package Dependencies

All CLI tools share one `go.mod` with zero third-party Go modules.
Packages that write text to GitHub PRs or commits pass outgoing payloads through `internal/leakcheck` (`betterleaks`) before sending them.

```mermaid
flowchart LR
  subgraph Entrypoints["pkgs/* and cmd/*"]
    automerge["pkgs/automerge"]
    bump["pkgs/bump-flake-lock"]
    cleanup["pkgs/cleanup-ghcr"]
    minimize["pkgs/minimize-pr-comments"]
    nixdiff["pkgs/nix-diff-closures"]
    cachixpin["pkgs/cachixpin"]
    schedule["pkgs/recommend-schedule"]
    wflint["cmd/workflowlint"]
  end

  subgraph Shared["Shared internal packages"]
    gh["internal/gh (REST + GraphQL client)"]
    leakcheck["internal/leakcheck (betterleaks pre-flight scan)"]
    wfmeta["internal/workflowmeta (provenance metadata)"]
  end

  subgraph NixDiffInternal["pkgs/nix-diff-closures/internal/*"]
    ndDiff["internal/diff"]
    ndOsv["internal/osv"]
    ndReport["internal/report"]
  end

  automerge --> leakcheck
  automerge --> wfmeta

  bump --> leakcheck
  bump --> wfmeta

  cleanup --> gh
  cleanup --> leakcheck
  cleanup --> wfmeta

  minimize --> gh
  minimize --> wfmeta

  nixdiff --> gh
  nixdiff --> leakcheck
  nixdiff --> wfmeta
  nixdiff --> ndDiff
  nixdiff --> ndOsv
  nixdiff --> ndReport
  nixdiff -.->|"execs wrapped binary"| minimize
  ndDiff --> ndReport
  ndReport --> ndOsv
  ndReport --> wfmeta
```

## Package-Level Diagrams

Each workflow package documents its own control flow and guardrails:

- [automerge](../pkgs/automerge/README.md)
- [betterleaks](../pkgs/betterleaks/README.md)
- [bump-flake-lock](../pkgs/bump-flake-lock/README.md)
- [cleanup-ghcr](../pkgs/cleanup-ghcr/README.md)
- [minimize-pr-comments](../pkgs/minimize-pr-comments/README.md)
- [nix-diff-closures](../pkgs/nix-diff-closures/README.md)
