#!/bin/bash
# One installer for a new OpenVMS host. The SSH password never appears here.
# Subcommands: detect, packages, compose, ntp, agent.
set -euo pipefail

ROOT=/opt/openvms/agent
FRIGATE=/opt/openvms/frigate

detect() {
  local coral=0 nvidia=0 openvino=0 vendor
  if [[ -e /dev/apex_0 ]]; then coral=1; fi
  if command -v lsusb >/dev/null 2>&1 && lsusb | grep -Eiq '1a6e:089a|18d1:9302'; then
    coral=1
  fi
  if [[ -e /dev/nvidia0 ]]; then nvidia=1; fi
  if command -v nvidia-smi >/dev/null 2>&1 && nvidia-smi -L >/dev/null 2>&1; then
    nvidia=1
  fi
  if grep -q GenuineIntel /proc/cpuinfo 2>/dev/null; then openvino=1; fi
  if [[ -d /sys/class/drm ]]; then
    for vendor in /sys/class/drm/card*/device/vendor; do
      if [[ -f "$vendor" ]] && grep -qi '0x8086' "$vendor"; then openvino=1; fi
    done
  fi
  echo "coral=${coral} nvidia=${nvidia} openvino=${openvino}"
}

packages() {
  export DEBIAN_FRONTEND=noninteractive
  apt-get update
  apt-get upgrade -y
  apt-get install -y docker.io docker-compose nodejs npm curl ca-certificates chrony usbutils
  systemctl enable --now docker
}

compose_up() {
  local src cfg files
  case "${VARIANT:-}" in
    coral)
      src="${ROOT}/compose/frigate-stable.yml"
      cfg="${ROOT}/config/coral.yml"
      ;;
    tensorrt)
      src="${ROOT}/compose/frigate-tensorrt.yml"
      cfg="${ROOT}/config/tensorrt.yml"
      ;;
    openvino)
      src="${ROOT}/compose/frigate-openvino.yml"
      cfg="${ROOT}/config/openvino.yml"
      ;;
    cpu)
      src="${ROOT}/compose/frigate-cpu.yml"
      cfg="${ROOT}/config/cpu.yml"
      ;;
    *)
      echo "VARIANT must be coral, tensorrt, openvino, or cpu" >&2
      exit 1
      ;;
  esac
  if [[ "${VARIANT}" == tensorrt ]] && command -v nvidia-ctk >/dev/null 2>&1; then
    nvidia-ctk runtime configure --runtime=docker
    systemctl restart docker
  elif [[ "${VARIANT}" == tensorrt ]]; then
    apt-get install -y nvidia-container-toolkit || true
    if command -v nvidia-ctk >/dev/null 2>&1; then
      nvidia-ctk runtime configure --runtime=docker
      systemctl restart docker
    fi
  fi
  mkdir -p "${FRIGATE}/config" "${FRIGATE}/storage"
  cp "${src}" "${FRIGATE}/compose.yml"
  cp "${cfg}" "${FRIGATE}/config/config.yml"
  if [[ "${VARIANT}" == coral && -e /dev/apex_0 ]]; then
    if ! command -v lsusb >/dev/null 2>&1 || ! lsusb | grep -Eiq '1a6e:089a|18d1:9302'; then
      sed -i 's/device: usb/device: pci/' "${FRIGATE}/config/config.yml"
    fi
  fi
  files=(-f compose.yml)
  if [[ "${VARIANT}" == openvino && -d /dev/dri ]]; then
    cat > "${FRIGATE}/compose.dri.yml" <<'EOF'
services:
  frigate:
    devices:
      - /dev/dri:/dev/dri
EOF
    files+=(-f compose.dri.yml)
  fi
  cd "${FRIGATE}"
  if command -v docker-compose >/dev/null 2>&1; then
    docker-compose "${files[@]}" up -d
  else
    docker compose "${files[@]}" up -d
  fi
  local i
  for i in $(seq 1 120); do
    if curl -fsS http://127.0.0.1:5000/api/version >/dev/null; then
      return 0
    fi
    sleep 5
  done
  echo "Frigate did not answer on port 5000" >&2
  exit 1
}

ntp_server() {
  local svc=chrony
  mkdir -p /etc/chrony/conf.d
  if [[ -f /etc/chrony/chrony.conf ]] && ! grep -q '^confdir ' /etc/chrony/chrony.conf; then
    echo 'confdir /etc/chrony/conf.d' >> /etc/chrony/chrony.conf
  fi
  cp "${ROOT}/chrony.conf" /etc/chrony/conf.d/openvms.conf
  if ! systemctl cat chrony >/dev/null 2>&1; then
    svc=chronyd
  fi
  systemctl enable --now "${svc}"
  systemctl restart "${svc}"
  systemctl is-active "${svc}"
}

agent_service() {
  if [[ ! -x /usr/local/bin/openvms-agent ]]; then
    echo "openvms-agent binary is missing" >&2
    exit 1
  fi
  if [[ ! -f /etc/openvms/agent.token ]]; then
    echo "agent token file is missing" >&2
    exit 1
  fi
  mkdir -p /etc/openvms
  cat > /etc/openvms/agent.env <<EOF
OPENVMS_AGENT_VARIANT=${VARIANT:-cpu}
OPENVMS_AGENT_LISTEN=0.0.0.0:7419
OPENVMS_AGENT_TOKEN_FILE=/etc/openvms/agent.token
OPENVMS_CCTV_PATH=/opt/openvms/frigate/storage
OPENVMS_DB_PATH=/
EOF
  chmod 644 /etc/openvms/agent.env
  chmod 600 /etc/openvms/agent.token
  cp "${ROOT}/openvms-agent.service" /etc/systemd/system/openvms-agent.service
  systemctl daemon-reload
  systemctl enable --now openvms-agent
  systemctl is-active openvms-agent
}

case "${1:-}" in
  detect) detect ;;
  packages) packages ;;
  compose) compose_up ;;
  ntp) ntp_server ;;
  agent) agent_service ;;
  *)
    echo "usage: install.sh detect|packages|compose|ntp|agent" >&2
    exit 1
    ;;
esac
