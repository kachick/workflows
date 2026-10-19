# bump-flake-lock

Update `flake.lock` and create a pull request with GitHub App.

Optionally runs a command after `flake.lock` is updated (for example, to update related CI dependencies) and commits the generated changes before opening the pull request.

## Usage

### Reusable Workflow

```yaml
name: Bump Dependencies

permissions:
  contents: read

on:
  schedule:
    # Run once a month on the 1st with distributed hour and minute
    - cron: '17 3 1 * *'
  workflow_dispatch:

jobs:
  bump:
    uses: kachick/workflows/.github/workflows/reusable-bump-flake-lock.yml@main
    with:
      app-id: ${{ vars.APP_ID }}
      # Optional: run custom package to update related files
      post-commit-lock-file: nix run .#update-deps
      post-commit-lock-file-title: 'Sync CI dependencies with nixpkgs'
    secrets:
      APP_PRIVATE_KEY: ${{ secrets.APP_PRIVATE_KEY }}
```

### Tips: Distribute Schedules Across Repositories

When running `bump-flake-lock` across multiple repositories, running them on the 1st of every month keeps your local Nix store size consistent (shared inputs). Distribute the minute (0-59) and hour (0-5) across repositories so GitHub App API rate limits and runner contention are avoided:

```console
# Calculate deterministic minute (0-59) and hour (0-5) using Ruby
ruby -e '
  require "zlib"
  crc = Zlib.crc32("kachick/workflows")
  min = crc % 60
  hour = (crc / 60) % 6
  puts "#{min} #{hour} 1 * *"
'
```

### CLI

```console
nix run github:kachick/workflows#bump-flake-lock -- --help
```
