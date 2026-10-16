# Mergeable Status: UNKNOWN

Automerge skipped this pull request because its mergeable status is `UNKNOWN`.

## What it means

GitHub computes pull request mergeability asynchronously in the background.

When a pull request is created or when new commits are pushed to the target branch (such as `main`), GitHub invalidates its mergeability cache and starts a background job to create a test merge commit. While this calculation runs, GitHub returns `UNKNOWN` (or `null` in the REST API).

## What to do

In most cases, **no manual action is needed**:

- The background calculation usually finishes in a few seconds to a few minutes.
- Automerge retries short calculations automatically during the workflow run.
- If it still remains `UNKNOWN` after retries, the next scheduled automerge run will inspect and process the pull request once GitHub completes the calculation.

## References

- [GitHub REST API: Get a pull request](https://docs.github.com/en/rest/pulls/pulls#get-a-pull-request)
- [GitHub GraphQL API: MergeableState enum](https://docs.github.com/en/graphql/reference/enums#mergeablestate)
