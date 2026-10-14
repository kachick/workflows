# workflows

Personal collection of reusable GitHub Actions workflows.

> [!NOTE]
> This repository is built for personal use cases.
> It is not intended to be a general-purpose workflow suite with wide options.

## Design and Common Rules

- **Reusable workflows (`workflow_call`)**:
  Shared jobs that caller repositories can invoke with minimal configuration.
- **Track `@main` branch**:
  Caller workflows specify `@main` to receive updates without tagging or manual release steps.
- **Powered by Nix**:
  Back-end CLI tools run directly via `nix run github:kachick/workflows#<target>` on Nix-enabled runners.
- **Runner**:
  Optimized for modern Ubuntu runners (such as `ubuntu-26.04`).

## Available Workflows

| Name             | Reusable Workflow                                                                                      | Description                                                                     | Details                                    |
| :--------------- | :----------------------------------------------------------------------------------------------------- | :------------------------------------------------------------------------------ | :----------------------------------------- |
| Nix Closure Diff | [`.github/workflows/reusable-nix-diff-closures.yml`](.github/workflows/reusable-nix-diff-closures.yml) | Post Nix package closure differences on pull requests when `flake.lock` changes | [README](pkgs/nix-diff-closures/README.md) |

## Repository Layout

- `.github/workflows/`: Reusable workflows (`workflow_call`) and repository CI
- `pkgs/`: Dedicated CLI tools and documentation for each workflow
