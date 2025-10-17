#!/usr/bin/env bash
set -euo pipefail

# YouTube to MP3 API - Ubuntu installer
# - Installs dependencies (ffmpeg, yt-dlp, optionally redis)
# - Builds and installs ytmp3api to /usr/local/bin
# - Creates system user and systemd service
# - Sets up data and config directories

SVC_NAME=${SVC_NAME:-ytmp3api}
SVC_USER=${SVC_USER:-ytmp3}
SVC_GROUP=${SVC_GROUP:-ytmp3}
INSTALL_BIN=${INSTALL_BIN:-/usr/local/bin/${SVC_NAME}}
DATA_DIR=${DATA_DIR:-/var/lib/${SVC_NAME}}
CONFIG_DIR=${CONFIG_DIR:-/etc/${SVC_NAME}}
ENV_FILE=${ENV_FILE:-${CONFIG_DIR}/${SVC_NAME}.env}
PORT=${PORT:-8080}
INSTALL_REDIS=${INSTALL_REDIS:-1}
REPO_DIR=${REPO_DIR:-$(pwd)}
GO_VERSION=${GO_VERSION:-1.22.8}

need_root() {
  if [[ ${EUID} -ne 0 ]]; then
    echo "[!] Please run as root (sudo)." >&2
    exit 1
  fi
}

apt_install() {
  export DEBIAN_FRONTEND=noninteractive
  apt-get update -y
  apt-get install -y --no-install-recommends \
    ca-certificates curl jq ffmpeg coreutils tar
  if [[ "${INSTALL_REDIS}" == "1" ]]; then
    apt-get install -y --no-install-recommends redis-server
    systemctl enable --now redis-server || true
  fi
}

install_yt_dlp() {
  if command -v yt-dlp >/dev/null 2>&1; then
    echo "[+] yt-dlp already installed: $(command -v yt-dlp)"
    return
  fi
  echo "[+] Installing yt-dlp to /usr/local/bin"
  curl -fsSL https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp -o /usr/local/bin/yt-dlp
  chmod a+rx /usr/local/bin/yt-dlp
}

install_go_if_needed() {
  if command -v go >/dev/null 2>&1; then
    echo "[+] go found: $(go version)"
    return
  fi
  echo "[+] Installing Go ${GO_VERSION}"
  ARCH=$(dpkg --print-architecture)
  case "$ARCH" in
    amd64|x86_64) GOARCH=amd64 ;;
    arm64|aarch64) GOARCH=arm64 ;;
    *) echo "[!] Unsupported architecture: $ARCH" >&2; exit 1 ;;
  esac
  URL="https://go.dev/dl/go${GO_VERSION}.linux-${GOARCH}.tar.gz"
  TMP="/tmp/go${GO_VERSION}.tar.gz"
  curl -fsSL "$URL" -o "$TMP"
  rm -rf /usr/local/go
  tar -C /usr/local -xzf "$TMP"
}

ensure_user() {
  if ! id -u "${SVC_USER}" >/dev/null 2>&1; then
    useradd --system --create-home --home-dir "/home/${SVC_USER}" --shell /usr/sbin/nologin "${SVC_USER}"
  fi
  mkdir -p "${DATA_DIR}" "${CONFIG_DIR}"
  chown -R "${SVC_USER}:${SVC_GROUP}" "${DATA_DIR}"
  chmod 0755 "${DATA_DIR}"
}

write_env_file() {
  if [[ -f "${ENV_FILE}" ]]; then
    echo "[+] Env file exists: ${ENV_FILE}"
    return
  fi
  cat >"${ENV_FILE}" <<'EOF'
# Worker Configuration
WORKER_POOL_SIZE=20
JOB_QUEUE_CAPACITY=1000
MAX_JOB_RETRIES=3

# Rate Limiting
REQUESTS_PER_SECOND=100
BURST_SIZE=200
PER_IP_RPS=10
PER_IP_BURST=20

# Redis Configuration
REDIS_ADDR=localhost:6379
REDIS_PASSWORD=
REDIS_DB=0

# External Tool Settings
YTDLP_TIMEOUT=90s
FFMPEG_MIN_TIMEOUT=15m
FFMPEG_MAX_TIMEOUT=60m
FFMPEG_MODE=CBR
FFMPEG_CBR_BITRATE=192k
FFMPEG_VBR_Q=5
FFMPEG_THREADS=0

# Download Strategy
ALWAYS_DOWNLOAD=false
DOWNLOAD_THRESHOLD=10m
YTDLP_DOWNLOAD_CONCURRENCY=8
YTDLP_DOWNLOAD_TIMEOUT=30m

# File Management
CONVERSIONS_DIR=/var/lib/ytmp3api/conversions
UNCONVERTED_FILE_TTL=5m
CONVERTED_FILE_TTL=10m

# Security & CORS
REQUIRE_API_KEY=false
API_KEYS=
ALLOWED_ORIGINS=*
ADMIN_USER=admin
ADMIN_PASS=password

# External APIs
OEMBED_ENDPOINT=https://www.youtube.com/oembed
DURATION_API_ENDPOINT=https://ds2.ezsrv.net/api/getDuration

# Concurrency Limits
MAX_CONCURRENT_DOWNLOADS=20
MAX_CONCURRENT_CONVERSIONS=20

# Service port (used only by reverse proxies)
PORT=8080
EOF
  chmod 0644 "${ENV_FILE}"
}

build_binary() {
  if [[ -x "${INSTALL_BIN}" ]]; then
    echo "[+] Binary already installed at ${INSTALL_BIN}"
    return
  fi
  if [[ ! -d "${REPO_DIR}/cmd/ytmp3" ]]; then
    echo "[!] Repo not found at ${REPO_DIR}; cannot build. Copy binary to ${INSTALL_BIN} and rerun." >&2
    exit 1
  fi
  install_go_if_needed
  export PATH="/usr/local/go/bin:${PATH}"
  (cd "${REPO_DIR}" && go mod download)
  (cd "${REPO_DIR}" && go build -o "${INSTALL_BIN}" ./cmd/ytmp3)
  chmod a+rx "${INSTALL_BIN}"
}

write_service() {
  SVC_FILE="/etc/systemd/system/${SVC_NAME}.service"
  cat >"${SVC_FILE}" <<EOF
[Unit]
Description=YouTube to MP3 API
After=network-online.target
Wants=network-online.target

[Service]
User=${SVC_USER}
Group=${SVC_GROUP}
EnvironmentFile=${ENV_FILE}
Environment=PATH=/usr/local/bin:/usr/bin:/bin:/usr/local/go/bin
WorkingDirectory=${DATA_DIR}
ExecStart=${INSTALL_BIN}
Restart=always
RestartSec=2
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
EOF
  systemctl daemon-reload
  systemctl enable "${SVC_NAME}"
  systemctl restart "${SVC_NAME}"
}

configure_firewall() {
  if command -v ufw >/dev/null 2>&1; then
    if ufw status | grep -q "Status: active"; then
      ufw allow ${PORT}/tcp || true
    fi
  fi
}

main() {
  need_root
  apt_install
  install_yt_dlp
  ensure_user
  write_env_file
  build_binary
  write_service
  configure_firewall
  echo "[✓] ${SVC_NAME} installed and started."
  systemctl --no-pager --full status "${SVC_NAME}" || true
  echo "[i] Data dir: ${DATA_DIR}"
  echo "[i] Env file: ${ENV_FILE}"
  echo "[i] API on port ${PORT} (default 8080)"
}

main "$@"
