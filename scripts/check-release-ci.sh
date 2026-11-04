#!/usr/bin/env bash
set -euo pipefail

version=$(jq -r .version mygo.json)
if [[ "$GITHUB_REF_NAME" != "v$version" ]]; then
  echo "tag $GITHUB_REF_NAME does not match version $version" >&2
  exit 1
fi

sha=$(git rev-parse HEAD)
if ! gh api "repos/$GITHUB_REPOSITORY/actions/workflows/ci.yml/runs?branch=main&event=push&head_sha=$sha&status=success&per_page=1" |
  jq -e --arg sha "$sha" 'any(.workflow_runs[]; .head_sha == $sha and .head_branch == "main" and .event == "push" and .status == "completed" and .conclusion == "success")' >/dev/null; then
  echo "no successful main CI run for $sha; wait for main CI and rerun the tag workflow" >&2
  exit 1
fi
echo "main CI passed for $sha"
