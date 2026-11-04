#!/usr/bin/env bash
set -euo pipefail

bin=$(mktemp -d)
trap 'rm -rf "$bin"' EXIT
cat >"$bin/gh" <<'EOF'
#!/usr/bin/env bash
[[ ${API_FAILURE:-0} != 1 ]] || exit 1
printf '%s\n' "$CI_RUNS"
EOF
chmod +x "$bin/gh"

export PATH="$bin:$PATH" GITHUB_REPOSITORY=example/tulai API_FAILURE=0
export GITHUB_REF_NAME="v$(jq -r .version mygo.json)"
sha=$(git rev-parse HEAD)
valid=$(jq -cn --arg sha "$sha" '{workflow_runs:[{head_sha:$sha,head_branch:"main",event:"push",status:"completed",conclusion:"success"}]}')
export CI_RUNS="$valid"
bash scripts/check-release-ci.sh

for filter in \
  '.workflow_runs=[]' \
  '.workflow_runs[0].head_sha="other"' \
  '.workflow_runs[0].head_branch="other"' \
  '.workflow_runs[0].event="pull_request"' \
  '.workflow_runs[0].status="in_progress"' \
  '.workflow_runs[0].conclusion="failure"'; do
  CI_RUNS=$(jq -c "$filter" <<<"$valid")
  if bash scripts/check-release-ci.sh >/dev/null 2>&1; then
    echo "release gate accepted invalid CI: $filter" >&2
    exit 1
  fi
done
CI_RUNS="$valid"
if GITHUB_REF_NAME="${GITHUB_REF_NAME}-invalid" bash scripts/check-release-ci.sh >/dev/null 2>&1 ||
   API_FAILURE=1 bash scripts/check-release-ci.sh >/dev/null 2>&1; then
  echo "release gate accepted a wrong tag or failed API request" >&2
  exit 1
fi
echo "release gate checks passed"
