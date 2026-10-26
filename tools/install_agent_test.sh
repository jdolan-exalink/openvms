#!/usr/bin/env bash
# Tests deploy/agent/install-agent.sh against a local "release" served over HTTP, into a throwaway
# prefix, without systemd. It builds the real agent binary, so the checksum, version and token steps
# run for real.
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
script="$root/deploy/agent/install-agent.sh"
tmp=$(mktemp -d)
server_pid=""
cleanup() { [[ -n "$server_pid" ]] && kill "$server_pid" 2>/dev/null || true; rm -rf "$tmp"; }
trap cleanup EXIT
fail() { echo "FAIL: $*" >&2; exit 1; }

arch=$(go env GOARCH)
version=9.8.7
asset="openvms-agent_${version}_linux_${arch}"
rel="$tmp/release/agent-v$version"
mkdir -p "$rel"
(cd "$root" && CGO_ENABLED=0 go build -trimpath -ldflags "-X github.com/jdolan-exalink/openvms/internal/platform/buildinfo.Version=$version" -o "$rel/$asset" ./apps/edge-agent)
(cd "$rel" && sha256sum "$asset" >SHA256SUMS)

port=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()')
(cd "$tmp/release" && python3 -m http.server "$port" --bind 127.0.0.1 >/dev/null 2>&1) &
server_pid=$!
for _ in $(seq 1 50); do curl -fsS "http://127.0.0.1:$port/agent-v$version/SHA256SUMS" >/dev/null 2>&1 && break; sleep 0.1; done

run() { # run <prefix> [args...]
  local prefix=$1; shift
  OPENVMS_AGENT_PREFIX="$prefix" OPENVMS_AGENT_SKIP_SYSTEMD=1 OPENVMS_AGENT_NO_ROOT_CHECK=1 \
    OPENVMS_AGENT_BASE_URL="http://127.0.0.1:$port" bash "$script" "$@"
}

# 1. A clean install: verified binary, token, env, unit, state dir, and the OpenVMS token printed once.
p1="$tmp/p1"
out=$(run "$p1" --version "$version" 2>&1) || fail "install failed: $out"
bin="$p1/usr/local/bin/openvms-agent"
[[ -x "$bin" ]] || fail "binary not installed"
[[ "$("$bin" --version)" == "$version" ]] || fail "installed binary reports $("$bin" --version)"
[[ $(stat -c %a "$p1/etc/openvms/agent.token") == 600 ]] || fail "agent.token must be 0600"
[[ $(wc -c <"$p1/etc/openvms/agent.token") -ge 33 ]] || fail "agent.token is too short"
grep -q '^OPENVMS_AGENT_LISTEN=0.0.0.0:7419$' "$p1/etc/openvms/agent.env" || fail "agent.env: $(cat "$p1/etc/openvms/agent.env")"
grep -q 'ExecStart=/usr/local/bin/openvms-agent' "$p1/etc/systemd/system/openvms-agent.service" || fail "unit not written"
[[ -d "$p1/var/lib/openvms-agent" ]] || fail "state dir missing"
grep -q 'ovmsa_' <<<"$out" || fail "the OpenVMS token was not printed: $out"
secret=$(grep -o 'ovmsa_[0-9a-f_]*' <<<"$out" | head -1)
[[ -n "$secret" ]] || fail "no token in the output"
if grep -q "$(cat "$p1/etc/openvms/agent.token")" <<<"$out"; then fail "the legacy agent token must never be printed"; fi
keyfile="$p1/etc/openvms/openvms.apikey"
[[ "$(cat "$keyfile" 2>/dev/null)" == "$secret" ]] || fail "the printed key was not saved in $keyfile"
[[ $(stat -c %a "$keyfile") == 600 ]] || fail "the key file must be 0600"
grep -q 'Conectar agente' <<<"$out" || fail "the output does not say where to enter the key: $out"

# 2. Re-running is an upgrade in place: the same token file, the same OpenVMS token, no second one.
legacy_before=$(cat "$p1/etc/openvms/agent.token")
out2=$(run "$p1" --version "$version" 2>&1) || fail "second run failed: $out2"
[[ "$(cat "$p1/etc/openvms/agent.token")" == "$legacy_before" ]] || fail "re-running changed the agent token"
if grep -q 'ovmsa_[0-9a-f]\{12\}_[0-9a-f]\{64\}' <<<"$out2"; then fail "re-running printed a new OpenVMS token"; fi
grep -qi 'token rotate openvms' <<<"$out2" || fail "re-running should say how to get a new token: $out2"
grep -q 'openvms.apikey' <<<"$out2" || fail "re-running should say where the saved key is: $out2"
[[ "$(cat "$keyfile")" == "$secret" ]] || fail "re-running changed the saved key"
n=$("$bin" token list --file "$p1/etc/openvms/agent.tokens.json" | grep -c ' openvms ')
[[ "$n" == 1 ]] || fail "expected exactly one openvms token, found $n"

# 3. A custom port ends up in the environment file.
p2="$tmp/p2"
run "$p2" --version "$version" --port 8443 >/dev/null 2>&1 || fail "install with a port failed"
grep -q '^OPENVMS_AGENT_LISTEN=0.0.0.0:8443$' "$p2/etc/openvms/agent.env" || fail "port not applied"

# 4. A tampered release is refused and nothing is installed.
p3="$tmp/p3"
printf 'deadbeef  %s\n' "$asset" >"$rel/SHA256SUMS"
if out3=$(run "$p3" --version "$version" 2>&1); then fail "a binary that does not match SHA256SUMS was installed"; fi
grep -qi 'checksum' <<<"$out3" || fail "the refusal should mention the checksum: $out3"
[[ ! -e "$p3/usr/local/bin/openvms-agent" && ! -e "$p3/etc/openvms/agent.token" ]] || fail "a refused install left files behind"
(cd "$rel" && sha256sum "$asset" >SHA256SUMS)

# 5. A missing release is a clear error, not a half install.
p4="$tmp/p4"
if out4=$(run "$p4" --version 0.0.1 2>&1); then fail "installing a version that does not exist succeeded"; fi
[[ ! -e "$p4/usr/local/bin/openvms-agent" ]] || fail "a failed download left a binary"

# 6. The wrong architecture is refused before anything is written.
p5="$tmp/p5"
if out5=$(OPENVMS_AGENT_UNAME_M=riscv64 run "$p5" --version "$version" 2>&1); then fail "an unsupported architecture was accepted"; fi
grep -qi 'architecture' <<<"$out5" || fail "should mention the architecture: $out5"

# 7. Bad arguments.
if run "$tmp/p6" --port 99999 --version "$version" >/dev/null 2>&1; then fail "port 99999 accepted"; fi
if run "$tmp/p6" --version "not a version" >/dev/null 2>&1; then fail "a bad version string accepted"; fi
if run "$tmp/p6" --bogus >/dev/null 2>&1; then fail "an unknown option accepted"; fi

# 8. Uninstall removes the program and the unit, and keeps the configuration unless --purge.
# A saved original Frigate configuration is announced, with how to restore it.
mkdir -p "$p1/var/lib/openvms-agent/backup"
echo "cameras: {}" >"$p1/var/lib/openvms-agent/backup/frigate-config.original.yml"
out8=$(run "$p1" --uninstall 2>&1) || fail "uninstall failed"
grep -q 'frigate-config.original.yml' <<<"$out8" || fail "uninstall did not mention the saved original configuration: $out8"
grep -qi 'restart Frigate' <<<"$out8" || fail "uninstall did not say how to restore it: $out8"
grep -q -- '--purge' <<<"$out8" || fail "uninstall did not say --purge removes it: $out8"
[[ -f "$p1/var/lib/openvms-agent/backup/frigate-config.original.yml" ]] || fail "uninstall removed the saved original configuration"
out8b=$(run "$tmp/p-nobackup" --uninstall 2>&1) || fail "uninstall without a backup failed"
if grep -q 'frigate-config.original.yml' <<<"$out8b"; then fail "uninstall mentioned a backup that does not exist: $out8b"; fi
[[ ! -e "$bin" && ! -e "$p1/etc/systemd/system/openvms-agent.service" ]] || fail "uninstall left the program behind"
[[ -e "$p1/etc/openvms/agent.token" ]] || fail "uninstall without --purge removed the configuration"
run "$p1" --uninstall --purge >/dev/null 2>&1 || fail "purge failed"
[[ ! -e "$p1/etc/openvms" && ! -e "$p1/var/lib/openvms-agent" ]] || fail "--purge left configuration behind"

# 9. The copy a release publishes has its own version baked in and needs no --version.
baked="$tmp/install-agent.sh"
sed -e "s/^DEFAULT_VERSION=\"@AGENT_VERSION@\"\$/DEFAULT_VERSION=\"$version\"/" -e "s/agent-v@AGENT_VERSION@/agent-v$version/" "$script" >"$baked"
grep -q "^DEFAULT_VERSION=\"$version\"\$" "$baked" || fail "the release build did not bake the version in"
p7="$tmp/p7"
OPENVMS_AGENT_PREFIX="$p7" OPENVMS_AGENT_SKIP_SYSTEMD=1 OPENVMS_AGENT_NO_ROOT_CHECK=1 OPENVMS_AGENT_BASE_URL="http://127.0.0.1:$port" bash "$baked" >/dev/null 2>&1 || fail "the baked script failed"
[[ "$("$p7/usr/local/bin/openvms-agent" --version)" == "$version" ]] || fail "the baked script installed another version"

# 10. An upgrade whose health check fails puts the previous binary back, and a second failed run does
# not turn the broken binary into the "previous" one.
p8="$tmp/p8"
run "$p8" --version "$version" >/dev/null 2>&1 || fail "setup install failed"
good=$(sha256sum "$p8/usr/local/bin/openvms-agent" | awk '{print $1}')
rel2="$tmp/release/agent-v9.8.8"
mkdir -p "$rel2"
(cd "$root" && CGO_ENABLED=0 go build -trimpath -ldflags "-X github.com/jdolan-exalink/openvms/internal/platform/buildinfo.Version=9.8.8" -o "$rel2/openvms-agent_9.8.8_linux_${arch}" ./apps/edge-agent)
(cd "$rel2" && sha256sum "openvms-agent_9.8.8_linux_${arch}" >SHA256SUMS)
for attempt in 1 2; do
  if out8=$(OPENVMS_AGENT_HEALTH_CHECK_CMD=false OPENVMS_AGENT_HEALTH_WAIT=1 run "$p8" --version 9.8.8 2>&1); then fail "an upgrade that fails its health check reported success"; fi
  grep -qi 'previous' <<<"$out8" || fail "the failure should say the previous version is back: $out8"
  [[ "$("$p8/usr/local/bin/openvms-agent" --version)" == "$version" ]] || fail "attempt $attempt: the previous binary was not restored"
  [[ "$(sha256sum "$p8/usr/local/bin/openvms-agent" | awk '{print $1}')" == "$good" ]] || fail "attempt $attempt: the restored binary differs"
  [[ ! -e "$p8/usr/local/bin/openvms-agent.rollback" ]] || fail "a rollback file was left behind"
done
# A healthy upgrade keeps the old binary as .prev.
OPENVMS_AGENT_HEALTH_CHECK_CMD=true run "$p8" --version 9.8.8 >/dev/null 2>&1 || fail "a healthy upgrade failed"
[[ "$("$p8/usr/local/bin/openvms-agent" --version)" == "9.8.8" ]] || fail "the upgrade did not install 9.8.8"
[[ "$("$p8/usr/local/bin/openvms-agent.prev" --version)" == "$version" ]] || fail ".prev is not the previous version"

# 11. An explicit --port that disagrees with an existing agent.env is refused, not ignored; without
# --port the address printed is the one the agent really listens on.
p9="$tmp/p9"
run "$p9" --version "$version" --port 8443 >/dev/null 2>&1 || fail "setup install failed"
if out9=$(run "$p9" --version "$version" --port 9000 2>&1); then fail "a conflicting --port was accepted"; fi
grep -q '8443' <<<"$out9" || fail "the refusal should name the port in use: $out9"
out9b=$(OPENVMS_AGENT_HEALTH_CHECK_CMD=true run "$p9" --version "$version" 2>&1) || fail "re-run failed: $out9b"
if grep -q ':7419' <<<"$out9b"; then fail "the re-run talks about the default port instead of the real one: $out9b"; fi

# 12. Without --version the script asks the release list for the newest agent release.
p10="$tmp/p10"
printf '[{"tag_name": "v0.9.0"}, {"tag_name": "agent-v%s"}, {"tag_name": "agent-v0.1.0"}]\n' "$version" >"$tmp/release/releases.json"
OPENVMS_AGENT_API_URL="http://127.0.0.1:$port/releases.json" run "$p10" >/dev/null 2>&1 || fail "installing the newest release failed"
[[ "$("$p10/usr/local/bin/openvms-agent" --version)" == "$version" ]] || fail "installed another release than the newest agent release"

echo "install_agent_test: ok"
