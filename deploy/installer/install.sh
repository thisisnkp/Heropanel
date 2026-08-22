#!/bin/sh
# NexPanel bootstrap installer.
#
#   curl -fsSL https://get.nexpanel.io/install.sh | sh
#
# This thin, auditable bootstrap (POSIX sh) detects arch/OS just enough to fetch
# the correct, signature-verified `np-installer` binary, then hands off to it.
# All intelligent, stateful, rollback-capable logic lives in np-installer
# (docs/07). Kept deliberately small so it is easy to read before piping to sh.
set -eu

CHANNEL="${NP_CHANNEL:-stable}"
BASE_URL="${NP_BASE_URL:-https://get.nexpanel.io}"

# The ed25519 release public key, pinned into this script at release time. It is
# forwarded to np-installer, which refuses to install an npd/np-broker whose
# SHA256SUMS manifest is not signed by the matching private key (held offline).
# Empty in-tree; the release pipeline substitutes the real key. An operator can
# also override it via NP_RELEASE_PUBKEY.
PINNED_PUBKEY="${NP_RELEASE_PUBKEY:-xOREJDp6Uy1Qh2MFToIXrxiBayKaJ/hZvZXpo+IEkcc=}"

log()  { printf '\033[0;36m==>\033[0m %s\n' "$1"; }
err()  { printf '\033[0;31mError:\033[0m %s\n' "$1" >&2; exit 1; }

# 1. Must be root (creates users, installs packages, writes /etc, /opt).
[ "$(id -u)" -eq 0 ] || err "Please run as root (e.g. with sudo)."

# 2. Require systemd (the panel is managed via systemd units).
[ -d /run/systemd/system ] || err "systemd is required but was not detected."

# 3. Detect OS + architecture.
[ "$(uname -s)" = "Linux" ] || err "NexPanel supports Linux only."

case "$(uname -m)" in
  x86_64|amd64)        ARCH="amd64" ;;
  aarch64|arm64)       ARCH="arm64" ;;
  i386|i686)           ARCH="386" ;;
  *) err "Unsupported CPU architecture: $(uname -m)" ;;
esac

# 4. Require a downloader.
if command -v curl >/dev/null 2>&1; then
  DL="curl -fsSL"
elif command -v wget >/dev/null 2>&1; then
  DL="wget -qO-"
else
  err "Neither curl nor wget is available."
fi

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

BIN_URL="${BASE_URL}/${CHANNEL}/np-installer-linux-${ARCH}"
SIG_URL="${BIN_URL}.sig"
SHA_URL="${BIN_URL}.sha256"

log "Fetching np-installer (${CHANNEL}, linux/${ARCH})…"
$DL "$BIN_URL"  > "$TMP/np-installer"    || err "Failed to download np-installer."
$DL "$SHA_URL"  > "$TMP/np-installer.sha256" 2>/dev/null || true

# 5. Verify checksum (and signature, when available) before executing.
if [ -s "$TMP/np-installer.sha256" ] && command -v sha256sum >/dev/null 2>&1; then
  EXPECT="$(cut -d' ' -f1 "$TMP/np-installer.sha256")"
  ACTUAL="$(sha256sum "$TMP/np-installer" | cut -d' ' -f1)"
  [ "$EXPECT" = "$ACTUAL" ] || err "Checksum mismatch — refusing to run."
  log "Checksum verified."
fi

if command -v cosign >/dev/null 2>&1; then
  $DL "$SIG_URL" > "$TMP/np-installer.sig" 2>/dev/null || true
  if [ -s "$TMP/np-installer.sig" ]; then
    cosign verify-blob --key "${BASE_URL}/nexpanel.pub" \
      --signature "$TMP/np-installer.sig" "$TMP/np-installer" \
      || err "Signature verification failed — refusing to run."
    log "Signature verified."
  fi
fi

chmod +x "$TMP/np-installer"

# 6. Hand off. Extra args to install.sh are forwarded to np-installer. The
# pinned release key travels with it so the daemon binaries are signature-checked.
log "Starting installer…"
if [ -n "$PINNED_PUBKEY" ]; then
  exec "$TMP/np-installer" --channel "$CHANNEL" --pubkey "$PINNED_PUBKEY" "$@"
fi
exec "$TMP/np-installer" --channel "$CHANNEL" "$@"
