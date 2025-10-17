#!/usr/bin/env bash
set -euo pipefail

SVC_NAME=${SVC_NAME:-ytmp3api}
SVC_USER=${SVC_USER:-ytmp3}
SVC_GROUP=${SVC_GROUP:-ytmp3}
INSTALL_BIN=${INSTALL_BIN:-/usr/local/bin/${SVC_NAME}}
DATA_DIR=${DATA_DIR:-/var/lib/${SVC_NAME}}
CONFIG_DIR=${CONFIG_DIR:-/etc/${SVC_NAME}}
ENV_FILE=${ENV_FILE:-${CONFIG_DIR}/${SVC_NAME}.env}

need_root() {
  if [[ ${EUID} -ne 0 ]]; then
    echo "[!] Please run as root (sudo)." >&2
    exit 1
  fi
}

stop_service() {
  systemctl stop "${SVC_NAME}" || true
  systemctl disable "${SVC_NAME}" || true
  rm -f "/etc/systemd/system/${SVC_NAME}.service"
  systemctl daemon-reload || true
}

remove_files() {
  rm -f "${INSTALL_BIN}" || true
  rm -rf "${CONFIG_DIR}" || true
  # Comment next line if you want to keep data
  rm -rf "${DATA_DIR}" || true
}

remove_user() {
  if id -u "${SVC_USER}" >/dev/null 2>&1; then
    userdel -r "${SVC_USER}" || true
  fi
}

main() {
  need_root
  stop_service
  remove_files
  remove_user
  echo "[✓] ${SVC_NAME} uninstalled."
}

main "$@"
