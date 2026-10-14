#!/usr/bin/env bash

set -euo pipefail

branch_name="bot-update-flake-lock"
post_command=""
post_commit_title=""
pr_title="Bump flake.lock and related dependencies"
pr_body="This PR was generated automatically."
dry_run="false"

while [ $# -gt 0 ]; do
	case "$1" in
	--branch-name=*)
		branch_name="${1#*=}"
		shift
		;;
	--post-command=*)
		post_command="${1#*=}"
		shift
		;;
	--post-commit-title=*)
		post_commit_title="${1#*=}"
		shift
		;;
	--pr-title=*)
		pr_title="${1#*=}"
		shift
		;;
	--pr-body=*)
		pr_body="${1#*=}"
		shift
		;;
	--dry-run=*)
		dry_run="${1#*=}"
		shift
		;;
	*)
		echo "Unknown argument: $1" >&2
		exit 1
		;;
	esac
done

git config user.name "github-actions[bot]"
git config user.email "41898282+github-actions[bot]@users.noreply.github.com"
git switch -C "${branch_name}"

nix flake update --commit-lock-file

if [ -n "${post_command}" ]; then
	eval "${post_command}"
	if [ -n "$(git status --porcelain)" ]; then
		commit_title="${post_commit_title:-${post_command}}"
		git add -A
		git commit -m "${commit_title}"
	fi
fi

default_branch="$(git symbolic-ref --short refs/remotes/origin/HEAD 2>/dev/null | sed 's@^origin/@@' || true)"
default_branch="${default_branch:-main}"

count="$(git rev-list --count "origin/${default_branch}..")"
if [ "$count" -gt 0 ]; then
	echo "Found ${count} commit(s)."
	if [ "${dry_run}" = "true" ]; then
		echo "Dry run enabled. Skipping push and PR creation."
		exit 0
	fi

	echo "Pushing to ${branch_name}..."
	git push origin "${branch_name}" --force

	if ! gh pr view "${branch_name}" --json state --jq .state 2>/dev/null | grep -q "OPEN"; then
		gh pr create \
			--base "${default_branch}" \
			--head "${branch_name}" \
			--title "${pr_title}" \
			--body "${pr_body}"
	fi
else
	echo "No commits added. Nothing to push or create."
fi
