#!/usr/bin/env bash
# fast-infra bootstrap. Run on a fresh VPS as root or a user in the docker group.
set -euo pipefail
cd "$(dirname "$0")" || exit 1

# GitHub repo the release binaries are published from (override for a fork).
REPO="${FAST_INFRA_REPO:-gurkanucar/fast-infra}"

command -v docker >/dev/null || { echo "docker not found. Install Docker first: https://docs.docker.com/engine/install/"; exit 1; }
docker compose version >/dev/null 2>&1 || { echo "docker compose plugin not found."; exit 1; }

# Run a command as root, using sudo only when we are not already root.
as_root() {
  if [ "$(id -u)" -eq 0 ]; then "$@"; else sudo "$@"; fi
}

# Download URL to OUTFILE with curl or wget; returns non-zero if neither works.
fetch() {
  if command -v curl >/dev/null; then curl -fsSL "$1" -o "$2"
  elif command -v wget >/dev/null; then wget -qO "$2" "$1"
  else return 1; fi
}

# --- infra/.env -------------------------------------------------------------
if [ ! -f infra/.env ]; then
  cp infra/.env.example infra/.env
  read -rp "Base domain (e.g. example.com): " BASE_DOMAIN
  read -rp "Email (Let's Encrypt + OpenObserve login): " ACME_EMAIL
  PG_PASSWORD=$(openssl rand -hex 16)
  # OpenObserve enforces a password policy (>=1 lowercase, uppercase, digit and
  # special char). A bare hex string has no uppercase/special, so OpenObserve
  # panics on boot — append one of each class to the random part.
  OO_PASSWORD="$(openssl rand -hex 16)Aa1@"
  RABBITMQ_PASSWORD=$(openssl rand -hex 16)
  DOZZLE_PLAIN=$(openssl rand -hex 8)
  # apr1 htpasswd entry; escape $ as $$ for compose interpolation.
  DOZZLE_HASH=$(openssl passwd -apr1 "$DOZZLE_PLAIN" | sed 's/\$/\$\$/g')

  sed -i "s|^BASE_DOMAIN=.*|BASE_DOMAIN=${BASE_DOMAIN}|" infra/.env
  sed -i "s|^ACME_EMAIL=.*|ACME_EMAIL=${ACME_EMAIL}|" infra/.env
  sed -i "s|^PG_PASSWORD=.*|PG_PASSWORD=${PG_PASSWORD}|" infra/.env
  sed -i "s|^OO_PASSWORD=.*|OO_PASSWORD=${OO_PASSWORD}|" infra/.env
  sed -i "s|^RABBITMQ_PASSWORD=.*|RABBITMQ_PASSWORD=${RABBITMQ_PASSWORD}|" infra/.env
  sed -i "s|^DOZZLE_AUTH=.*|DOZZLE_AUTH=admin:${DOZZLE_HASH}|" infra/.env

  echo
  echo "Generated credentials (also stored in infra/.env):"
  echo "  OpenObserve  ${ACME_EMAIL} / ${OO_PASSWORD}   -> https://logs.${BASE_DOMAIN}"
  echo "  Postgres     postgres / ${PG_PASSWORD}"
  echo "  Dozzle       admin / ${DOZZLE_PLAIN}          -> https://tail.${BASE_DOMAIN}"
  echo "  Adminer      https://db.${BASE_DOMAIN} (login with postgres creds)"
  echo
fi

# --- CLI --------------------------------------------------------------------
# Prefer a prebuilt release binary (no Go needed); fall back to building.
install_cli() {
  local arch bin url
  bin=/usr/local/bin/platform
  case "$(uname -m)" in
    x86_64 | amd64) arch=amd64 ;;
    aarch64 | arm64) arch=arm64 ;;
    *) arch="" ;;
  esac

  if [ -n "$arch" ]; then
    url="https://github.com/${REPO}/releases/latest/download/platform-linux-${arch}"
    echo "Downloading platform ($arch) from the latest release..."
    if fetch "$url" /tmp/platform && [ -s /tmp/platform ]; then
      chmod +x /tmp/platform && as_root mv /tmp/platform "$bin"
      echo "Installed: $bin"
      return 0
    fi
    echo "Release download failed; falling back to building from source."
  fi

  if command -v go >/dev/null; then
    (cd cli && go build -o /tmp/platform .) && as_root mv /tmp/platform "$bin"
    echo "Installed (built from source): $bin"
  else
    echo "No release binary for this arch and Go is not installed."
    echo "Install Go and re-run, or build manually: cd cli && go build -o $bin ."
  fi
}
install_cli

# --- up ---------------------------------------------------------------------
echo "Starting infra..."
docker compose -f infra/docker-compose.yml up -d
echo
echo "Done. Next steps:"
echo "  1. Point DNS A records to this server: db/logs/tail (+ your app domains)."
echo "  2. Create your first app:   platform new myapp"
echo "  3. Deploy it:               platform deploy myapp latest"
echo "  Optional RabbitMQ:          docker compose -f infra/docker-compose.yml --profile rabbitmq up -d"
