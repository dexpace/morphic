#!/usr/bin/env bash
#
# install.sh installs the review agent for the current user: the agent into
# ~/.claude/agents, and the catalog it reads into ~/.claude/morphic-review.
#
# It installs per user rather than per project because .gitignore keeps .claude/
# out of the repository. The agent names the catalog by absolute path, so a
# review of an old revision, whose tree has no catalog, still finds one. Rerun
# after either file changes.
#
# Usage: install.sh
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
dest="${CLAUDE_CONFIG_DIR:-$HOME/.claude}"
catalog="$dest/morphic-review/catalog.md"

mkdir -p "$dest/agents" "$dest/morphic-review"
cp "$here/catalog.md" "$catalog"
sed "s#CATALOG_PATH#$catalog#" "$here/morphic-reviewer.md" >"$dest/agents/morphic-reviewer.md"
echo "installed $dest/agents/morphic-reviewer.md (catalog: $catalog)"
