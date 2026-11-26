#!/usr/bin/env bash
# Prepares Maflam Trading's Linux cloud box (Ubuntu/Debian) for its two StockCount /
# Vantage Suite stacks — testing and live run side by side as separate Docker Compose
# projects (maflam-testing, maflam-live). Run once as root; running it again is safe
# (e.g. to add a key):
#
#   sudo bash server-bootstrap.sh --user dev
#
# --user  the account GitHub Actions logs in as (default deploy; Maflam's box: dev). An
#         existing account is kept as it is; it is added to the docker group.
# --key   a public key to allow for that user; repeatable; optional when the user can
#         already log in with the deploy key (Maflam: dev already has the devtr key).
#
# Installs Docker (Docker's own install script), rsync and curl; creates the user "deploy"
# (docker group) and /opt/stocktake/testing and /opt/stocktake/live. No web server: the
# stacks sit behind the nginx already on the box (as 3retail-licensing, EDGE_MODE=proxy).
set -euo pipefail

DEPLOY_USER=deploy
BASE_DIR=/opt/stocktake
KEYS=()

while [ $# -gt 0 ]; do
  case "$1" in
    --user) DEPLOY_USER="${2:?--user needs a name}"; shift 2 ;;
    --key)  KEYS+=("${2:?--key needs a public key}"); shift 2 ;;
    *) echo "Unknown argument: $1 (see the top of this script)"; exit 1 ;;
  esac
done

[ "$(id -u)" -eq 0 ] || { echo "Run as root (sudo)."; exit 1; }
for k in "${KEYS[@]}"; do
  case "$k" in ssh-*|ecdsa-*) ;; *) echo "Not an SSH public key: $k"; exit 1 ;; esac
done

echo "→ Packages"
apt-get update -qq
apt-get install -y -qq ca-certificates curl rsync gnupg

if ! command -v docker >/dev/null 2>&1; then
  echo "→ Docker (get.docker.com)"
  curl -fsSL https://get.docker.com | sh
fi
systemctl enable --now docker
docker compose version >/dev/null || { echo "docker compose plugin missing"; exit 1; }

echo "→ User $DEPLOY_USER"
id "$DEPLOY_USER" >/dev/null 2>&1 || useradd --create-home --shell /bin/bash "$DEPLOY_USER"
usermod -aG docker "$DEPLOY_USER"
if [ ${#KEYS[@]} -gt 0 ]; then
  HOME_DIR="$(getent passwd "$DEPLOY_USER" | cut -d: -f6)"
  GROUP="$(id -gn "$DEPLOY_USER")"
  install -d -m 700 -o "$DEPLOY_USER" -g "$GROUP" "$HOME_DIR/.ssh"
  AUTH="$HOME_DIR/.ssh/authorized_keys"
  touch "$AUTH"
  for k in "${KEYS[@]}"; do grep -qxF "$k" "$AUTH" || echo "$k" >> "$AUTH"; done
  chown "$DEPLOY_USER:$GROUP" "$AUTH" && chmod 600 "$AUTH"
fi

echo "→ $BASE_DIR/testing and $BASE_DIR/live"
install -d -o "$DEPLOY_USER" -g "$(id -gn "$DEPLOY_USER")" "$BASE_DIR" "$BASE_DIR/testing" "$BASE_DIR/live"

echo
echo "✓ Box ready."
echo "  GitHub variables (both environments): SSH_HOST=<this box's address>  SSH_USER=$DEPLOY_USER"
echo "  maflam-testing: APP_DIR=$BASE_DIR/testing  HTTP_PORT=8085    maflam-live: APP_DIR=$BASE_DIR/live  HTTP_PORT=8084"
echo "  The box's nginx serves the domains: with nginx in docker set PROXY_NETWORK (setup-github.ps1"
echo "  finds it); with nginx on the host, proxy each domain to 127.0.0.1:8085 / 127.0.0.1:8084."
