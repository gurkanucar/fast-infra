#!/usr/bin/env bash
# fast-infra bootstrap. Run on a fresh VPS as a user in the docker group.
set -euo pipefail
cd "$(dirname "$0")"

command -v docker >/dev/null || { echo "docker not found. Install Docker first: https://docs.docker.com/engine/install/"; exit 1; }
docker compose version >/dev/null 2>&1 || { echo "docker compose plugin not found."; exit 1; }

# --- infra/.env -------------------------------------------------------------
if [ ! -f infra/.env ]; then
  cp infra/.env.example infra/.env
  read -rp "Base domain (e.g. example.com): " BASE_DOMAIN
  read -rp "Email (Let's Encrypt + OpenObserve login): " ACME_EMAIL
  PG_PASSWORD=$(openssl rand -hex 16)
  OO_PASSWORD=$(openssl rand -hex 16)
  RABBITMQ_PASSWORD=$(openssl rand -hex 16)
  DOZZLE_PLAIN=$(openssl rand -hex 8)
  # apr1 htpasswd entry; escape $ as $$ for compose interpolation
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
if command -v go >/dev/null; then
  echo "Building platform CLI..."
  (cd cli && go build -o /tmp/platform .) && sudo mv /tmp/platform /usr/local/bin/platform
  echo "Installed: /usr/local/bin/platform"
else
  echo "Go not found; skipping CLI build. Install Go and run: cd cli && go build -o /usr/local/bin/platform ."
fi

# --- up ---------------------------------------------------------------------
echo "Starting infra..."
docker compose -f infra/docker-compose.yml up -d
echo
echo "Done. Next steps:"
echo "  1. Point DNS A records to this server: db/logs/tail (+ your app domains)."
echo "  2. Create your first app:   platform new myapp"
echo "  3. Deploy it:               platform deploy myapp latest"
echo "  Optional RabbitMQ:          docker compose -f infra/docker-compose.yml --profile rabbitmq up -d"
