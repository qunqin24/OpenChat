#!/usr/bin/env bash
# Fails if a migration added since BASE sorts at or below the newest migration
# BASE already had.
#
#   scripts/check-migration-order.sh <base-ref>
#
# goose refuses to start on a database that has run a higher version than one
# it has not, so a migration numbered below one already released would stop
# every upgraded server from booting — while a fresh database, which is all CI
# tests on, would be fine. `make migration` numbers past the newest; this
# catches a file named by hand, or two branches that each added one.
set -euo pipefail

base="${1:?usage: check-migration-order.sh <base-ref>}"
dir=apps/server/internal/db/migrations
version() { sed -n 's#^.*/\([0-9]\{14\}\)_[^/]*\.sql$#\1#p'; }

newest=$(git ls-tree -r --name-only "$base" -- "$dir" | version | sort | tail -1)
added=$(git diff --name-only --diff-filter=A "$base" HEAD -- "$dir" | version | sort)

status=0
for v in $added; do
  if [[ -n "$newest" && "$v" -le "$newest" ]]; then
    echo "::error::migration $v sorts at or below $newest, the newest on $base; renumber it above (make migration does)"
    status=1
  fi
done
[[ $status -eq 0 ]] && echo "migrations in order (newest on $base: ${newest:-none}; added: ${added:-none})"
exit $status
