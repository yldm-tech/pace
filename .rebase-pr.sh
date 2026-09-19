#!/bin/bash
# Rebase a PR branch onto origin/main, auto-resolving the workspace version bump
# that every branch carries and that therefore conflicts on every merge. Any
# conflict outside those package.json files stops the script for a human read.
set -uo pipefail
cd /Users/e-hu/Workspace/pace
BRANCH="$1"; VERSION="$2"
git checkout "$BRANCH" >/dev/null 2>&1 || exit 1
git rebase origin/main >/dev/null 2>&1
while [ -d .git/rebase-merge ] || [ -d .git/rebase-apply ]; do
  UNMERGED=$(git diff --diff-filter=U --name-only)
  if [ -z "$UNMERGED" ]; then
    git -c core.editor=true rebase --continue >/dev/null 2>&1 || { echo "STUCK: rebase --continue failed"; exit 1; }
    continue
  fi
  OTHER=$(echo "$UNMERGED" | grep -v -E '(^|/)package\.json$' || true)
  if [ -n "$OTHER" ]; then
    echo "MANUAL CONFLICT:"; echo "$OTHER"; exit 2
  fi
  echo "$UNMERGED" | xargs git checkout --ours --
  ./.setver.sh "$VERSION" >/dev/null
  echo "$UNMERGED" | xargs git add
  git -c core.editor=true rebase --continue >/dev/null 2>&1 || { echo "STUCK after resolve"; exit 1; }
done
# The bump commit may have been rebased before the version reached its final value.
CURRENT=$(node -p "require('/Users/e-hu/Workspace/pace/package.json').version")
if [ "$CURRENT" != "$VERSION" ]; then
  ./.setver.sh "$VERSION" >/dev/null
  git commit -aqm "chore: bump the workspace version, which check-version requires of every PR"
fi
echo "REBASED $BRANCH onto $(git rev-parse --short origin/main) at version $VERSION"
git log --oneline origin/main..HEAD
