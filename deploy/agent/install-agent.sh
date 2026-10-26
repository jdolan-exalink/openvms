#!/usr/bin/env bash
# Installs or upgrades the OpenVMS edge agent from a GitHub release.
#
#   curl -fsSL https://github.com/jdolan-exalink/openvms/releases/download/agent-v@AGENT_VERSION@/install-agent.sh | sudo bash
#   curl -fsSL .../install-agent.sh | sudo bash -s -- --port 7419
#
# It downloads the binary for this machine, verifies it against the release's SHA256SUMS, installs it as a
# systemd service, creates the agent token on the first run and prints the address and the token to
# register in OpenVMS. Running it again upgrades in place and keeps the tokens.
#
# Options: --version X.Y.Z   --port N (default 7419)   --uninstall [--purge]   --help
set -euo pipefail

DEFAULT_VERSION="@AGENT_VERSION@"
REPO="jdolan-exalink/openvms"
BASE_URL="${OPENVMS_AGENT_BASE_URL:-https://github.com/$REPO/releases/download}"
PREFIX="${OPENVMS_AGENT_PREFIX:-}"

VERSION="${OPENVMS_AGENT_VERSION:-$DEFAULT_VERSION}"
PORT=7419
PORT_EXPLICIT=0
ACTION=install
PURGE=0

BIN_DIR="$PREFIX/usr/local/bin"
BIN="$BIN_DIR/openvms-agent"
CONF_DIR="$PREFIX/etc/openvms"
STATE_DIR="$PREFIX/var/lib/openvms-agent"
UNIT_FILE="$PREFIX/etc/systemd/system/openvms-agent.service"

say() { printf '%s\n' "$*"; }
die() { printf 'install-agent: %s\n' "$*" >&2; exit 1; }

usage() {
  cat <<'USAGE'
Installs or upgrades the OpenVMS edge agent from a GitHub release.

  curl -fsSL https://github.com/jdolan-exalink/openvms/releases/download/agent-vX.Y.Z/install-agent.sh | sudo bash
  curl -fsSL .../install-agent.sh | sudo bash -s -- --port 7419

Options:
  --version X.Y.Z   install that agent release (default: the release the script came from)
  --port N          port the agent listens on on the first install (default 7419)
  --uninstall       remove the program and the service, keep the configuration
  --purge           with --uninstall, also remove /etc/openvms and /var/lib/openvms-agent
  --help            this text
USAGE
  exit "${1:-0}"
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --version) [[ $# -ge 2 ]] || die "--version needs a value"; VERSION=$2; shift 2 ;;
    --port) [[ $# -ge 2 ]] || die "--port needs a value"; PORT=$2; PORT_EXPLICIT=1; shift 2 ;;
    --uninstall) ACTION=uninstall; shift ;;
    --purge) PURGE=1; shift ;;
    --help | -h) usage 0 ;;
    *) die "unknown option: $1 (try --help)" ;;
  esac
done

[[ "$PORT" =~ ^[0-9]+$ ]] && ((PORT >= 1 && PORT <= 65535)) || die "--port must be between 1 and 65535"

if [[ -z "${OPENVMS_AGENT_NO_ROOT_CHECK:-}" && $EUID -ne 0 ]]; then
  die "run it as root: ... | sudo bash"
fi

systemd() { [[ -n "${OPENVMS_AGENT_SKIP_SYSTEMD:-}" ]] || systemctl "$@"; }

# A host that already has an agent.env keeps it, so the port it names is the port the agent uses.
if [[ "$ACTION" == install && -f "$CONF_DIR/agent.env" ]]; then
  existing_port=$(sed -n 's/^OPENVMS_AGENT_LISTEN=.*:\([0-9][0-9]*\)$/\1/p' "$CONF_DIR/agent.env" | head -1)
  if [[ -n "$existing_port" ]]; then
    if ((PORT_EXPLICIT)) && [[ "$existing_port" != "$PORT" ]]; then
      die "$CONF_DIR/agent.env already sets port $existing_port; pass --port $existing_port or edit that file first"
    fi
    PORT=$existing_port
  fi
fi

if [[ "$ACTION" == uninstall ]]; then
  systemd disable --now openvms-agent 2>/dev/null || true
  rm -f "$UNIT_FILE" "$BIN" "$BIN.prev"
  systemd daemon-reload 2>/dev/null || true
  if ((PURGE)); then
    rm -rf "$CONF_DIR" "$STATE_DIR"
    say "Removed the agent and its configuration (tokens included)."
  else
    say "Removed the agent. Kept $CONF_DIR/ and $STATE_DIR/ (use --uninstall --purge to remove them)."
    if [[ -f "$STATE_DIR/backup/frigate-config.original.yml" ]]; then
      say "The original Frigate configuration is kept at $STATE_DIR/backup/frigate-config.original.yml: copy it over Frigate's config.yml and restart Frigate to restore it (--purge removes it)."
    fi
  fi
  exit 0
fi

# --- what we are installing -------------------------------------------------------------------
case "${OPENVMS_AGENT_UNAME_M:-$(uname -m)}" in
  x86_64 | amd64) ARCH=amd64 ;;
  aarch64 | arm64) ARCH=arm64 ;;
  *) die "unsupported architecture $(uname -m): the agent is published for amd64 and arm64" ;;
esac
[[ "$(uname -s)" == Linux || -n "${OPENVMS_AGENT_NO_ROOT_CHECK:-}" ]] || die "the agent runs on Linux only"
if [[ -z "${OPENVMS_AGENT_SKIP_SYSTEMD:-}" ]]; then
  command -v systemctl >/dev/null 2>&1 || die "systemd is required (systemctl not found)"
fi
command -v sha256sum >/dev/null 2>&1 || die "sha256sum is required"

fetch() { # fetch URL DEST
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL --retry 3 --connect-timeout 10 -o "$2" "$1"
  elif command -v wget >/dev/null 2>&1; then
    wget -q -O "$2" "$1"
  else
    die "curl or wget is required"
  fi
}

if [[ "$VERSION" == @*@ ]]; then
  # Running the script from the repository rather than from a release (the release build replaces
  # DEFAULT_VERSION above): use the newest agent release.
  tmp_api=$(mktemp)
  fetch "${OPENVMS_AGENT_API_URL:-https://api.github.com/repos/$REPO/releases?per_page=30}" "$tmp_api" || die "could not list the releases of $REPO"
  VERSION=$(grep -o '"tag_name": *"agent-v[0-9][0-9.]*"' "$tmp_api" | head -1 | sed 's/.*agent-v//; s/"//') || true
  rm -f "$tmp_api"
  [[ -n "$VERSION" ]] || die "no agent release found; pass --version"
fi
[[ "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "the version must look like 0.4.0"

ASSET="openvms-agent_${VERSION}_linux_${ARCH}"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

say "Installing OpenVMS agent $VERSION ($ARCH)..."
fetch "$BASE_URL/agent-v$VERSION/$ASSET" "$work/$ASSET" || die "could not download $ASSET (does release agent-v$VERSION exist?)"
fetch "$BASE_URL/agent-v$VERSION/SHA256SUMS" "$work/SHA256SUMS" || die "could not download SHA256SUMS for agent-v$VERSION"

expected=$(awk -v f="$ASSET" '$2 == f || $2 == "*" f { print $1; exit }' "$work/SHA256SUMS")
[[ -n "$expected" ]] || die "SHA256SUMS has no entry for $ASSET"
actual=$(sha256sum "$work/$ASSET" | awk '{print $1}')
[[ "$actual" == "$expected" ]] || die "checksum mismatch for $ASSET (expected $expected, got $actual): not installing"

chmod 0755 "$work/$ASSET"
reported=$("$work/$ASSET" --version 2>/dev/null || true)
[[ "$reported" == "$VERSION" ]] || die "the downloaded binary reports version '$reported', expected $VERSION: not installing"

# --- install ----------------------------------------------------------------------------------
umask 077
mkdir -p "$BIN_DIR" "$CONF_DIR" "$STATE_DIR" "$(dirname "$UNIT_FILE")"
chmod 0750 "$CONF_DIR"
chmod 0700 "$STATE_DIR"

# The binary being replaced is kept aside until the new one has answered: it only becomes ".prev" (the
# known-good one) after a healthy upgrade, so a failed run can neither lose it nor turn a broken
# binary into the "previous" one.
upgrading=0
if [[ -x "$BIN" ]]; then
  upgrading=1
  cp -p "$BIN" "$BIN.rollback"
fi
install -m 0755 "$work/$ASSET" "$BIN.new"
mv -f "$BIN.new" "$BIN"

# The legacy agent token: created once, never replaced or printed.
if [[ ! -s "$CONF_DIR/agent.token" ]]; then
  head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n' >"$CONF_DIR/agent.token"
  chmod 0600 "$CONF_DIR/agent.token"
fi

if [[ ! -f "$CONF_DIR/agent.env" ]]; then
  cat >"$CONF_DIR/agent.env" <<ENV
OPENVMS_AGENT_LISTEN=0.0.0.0:$PORT
OPENVMS_AGENT_TOKEN_FILE=/etc/openvms/agent.token
OPENVMS_AGENT_STATE_DIR=/var/lib/openvms-agent
ENV
  chmod 0640 "$CONF_DIR/agent.env"
else
  say "Kept $CONF_DIR/agent.env (the agent listens on port $PORT)."
fi

cat >"$UNIT_FILE" <<'UNIT'
[Unit]
Description=OpenVMS edge agent
After=network-online.target docker.service
Wants=network-online.target

[Service]
EnvironmentFile=/etc/openvms/agent.env
ExecStart=/usr/local/bin/openvms-agent
Restart=on-failure
RestartSec=2

[Install]
WantedBy=multi-user.target
UNIT
chmod 0644 "$UNIT_FILE"

systemd daemon-reload
systemd enable openvms-agent >/dev/null 2>&1
systemd restart openvms-agent

healthy() {
  if [[ -n "${OPENVMS_AGENT_HEALTH_CHECK_CMD:-}" ]]; then
    bash -c "$OPENVMS_AGENT_HEALTH_CHECK_CMD"
  elif [[ -n "${OPENVMS_AGENT_SKIP_SYSTEMD:-}" ]]; then
    return 0
  else
    curl -fsS -o /dev/null -H "Authorization: Bearer $(cat "$CONF_DIR/agent.token")" "http://127.0.0.1:$PORT/v1/metrics" 2>/dev/null
  fi
}

ok=0
for _ in $(seq 1 "${OPENVMS_AGENT_HEALTH_WAIT:-30}"); do
  if healthy; then ok=1; break; fi
  sleep 1
done
if ((!ok)); then
  if ((upgrading)); then
    mv -f "$BIN.rollback" "$BIN"
    systemd restart openvms-agent || true
    die "the new agent did not answer on port $PORT; the previous version is back. See: journalctl -u openvms-agent -n 50"
  fi
  rm -f "$BIN.rollback"
  die "the agent was installed but did not answer on port $PORT; see: journalctl -u openvms-agent -n 50"
fi
if ((upgrading)); then mv -f "$BIN.rollback" "$BIN.prev"; fi

if ((upgrading)); then say "Upgraded to $VERSION (the previous binary is kept as $BIN.prev)."; else say "Installed $VERSION."; fi

# --- what to enter in OpenVMS -----------------------------------------------------------------
if conn=$("$BIN" connection --port "$PORT" --file "$CONF_DIR/agent.tokens.json" 2>&1); then
  say ""
  say "$conn"
elif [[ -s "$CONF_DIR/openvms.apikey" ]]; then
  say ""
  say "The OpenVMS key already exists on this host: sudo cat $CONF_DIR/openvms.apikey"
  say "In OpenVMS: Servers > the server > \"Conectar agente\" (address of this host, port $PORT, and that key)."
  say "To replace it: sudo openvms-agent token rotate openvms"
else
  say ""
  say "An OpenVMS token already exists on this host and its key was not saved here."
  say "To get a new one: sudo openvms-agent token rotate openvms"
fi
