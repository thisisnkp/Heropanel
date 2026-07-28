#!/bin/bash
# Container shim for `nft` (real kernel nftables needs NET_ADMIN + modules and
# would disturb the container's own networking). It is enough of an nft to
# prove HeroPanel's rollback logic, which is the panel's actual contribution:
# a "current ruleset" file that `nft -f` replaces and `nft list ruleset` prints.
# The kernel's enforcement of a loaded ruleset is a distro concern, not ours.
state=/tmp/nft-current.ruleset
[ -f "$state" ] || echo "table inet baseline { chain input { type filter hook input priority 0; policy accept; } }" > "$state"

case "${1:-}" in
  list)
    cat "$state" ;;
  -f)
    # Load a ruleset file: it becomes the current ruleset (atomic replace).
    if [ -f "${2:-}" ]; then cp "$2" "$state"; exit 0; else echo "nft: no such file ${2:-}" >&2; exit 1; fi ;;
  *)
    exit 0 ;;
esac
