# Mergeable Status: CONFLICTING

Automerge skipped this pull request because it has merge conflicts with the target branch.

## What it means

The pull request branch conflicts with the latest state of the target branch (such as `main`). GitHub cannot automatically merge or rebase it.

## What to do

Resolve the merge conflict manually or trigger a rebase:

- For Dependabot pull requests, comment `@dependabot rebase` on the pull request.
- For Renovate pull requests, select the rebase checkbox in the pull request description or trigger a rebase from your Renovate dashboard.
- For other bot pull requests, update the branch locally or rebase it on the target branch.

## References

- [GitHub Docs: Resolving a merge conflict on GitHub](https://docs.github.com/en/pull-requests/collaborating-with-pull-requests/addressing-merge-conflicts/resolving-a-merge-conflict-on-github)
- [GitHub GraphQL API: MergeableState enum](https://docs.github.com/en/graphql/reference/enums#mergeablestate)
