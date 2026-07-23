# fast-infra

Deploy any containerized app (Go, Spring Boot, FastAPI, Express, React, static HTML...) to a single VPS with zero-downtime rolling deploys, automatic HTTPS, logs/traces, and one shared Postgres + Redis — with as few decisions as possible.

The target experience:

1. `./install.sh` on the VPS
2. Point DNS at the server
3. `platform new blog`
4. Add a 10-line workflow to your app repo
5. `git push` → your app is live on HTTPS, with no downtime on the next push

No Kubernetes, no daemon, no web UI. A ~4GB VPS comfortably runs the platform plus a handful of apps.

## What you get

| Component | Purpose |
|---|---|
| Traefik v3 | Reverse proxy, automatic Let's Encrypt HTTPS, discovers apps via Docker labels |
| PostgreSQL 16 | One instance, one database per app (`db.<domain>` → Adminer UI) |
| Nightly backups | pg_dump of all databases, 7 daily / 2 weekly / 1 monthly retention |
| Redis 7 | Shared cache, 256MB cap, LRU eviction — use key prefixes per app |
| OpenObserve | Logs + traces + metrics in one binary, OTLP-native, 14-day retention (`logs.<domain>`) |
| Dozzle | Live `docker logs -f` in the browser (`tail.<domain>`, basic-auth) |
| RabbitMQ | Optional, off by default (`--profile rabbitmq`) |
| `platform` CLI | Single static Go binary: new / deploy / rollback / scale / status |

## Install (on the VPS)

Requirements: a Linux VPS (~4GB RAM recommended, amd64 or arm64), Docker + compose plugin, and a domain you control. The install script downloads a prebuilt `platform` binary; Go is only needed as a fallback (unusual arch, or building from source).

```bash
git clone https://github.com/YOUR_USER/fast-infra ~/fast-infra
cd ~/fast-infra && ./install.sh
```

The script asks for your base domain and email, generates all passwords, installs the `platform` binary (downloading the prebuilt release for your arch, or building it if you have Go), and starts the infra stack. Add DNS A records for `db`, `logs`, `tail` (and later, each app's domain) pointing to the server. Also enable 2GB of swap if your provider image doesn't (`fallocate -l 2G /swapfile && chmod 600 /swapfile && mkswap /swapfile && swapon /swapfile`).

## Creating and deploying an app

```bash
cd ~/fast-infra
platform new blog          # asks image / domain / port / health path
docker exec -it fast-infra-postgres-1 createdb -U postgres blog
platform deploy blog v1    # or any image tag / commit SHA
```

Each app lives in `apps/<name>/` as three files: `app.yaml` (the definition), `.env` (secrets, chmod 600, gitignored), and a generated `docker-compose.yml`. Edit `app.yaml`, re-run `platform deploy`, done. If you need compose features the template doesn't cover, set `manual: true` in `app.yaml` and the platform will never touch your compose file again.

`app.yaml` reference:

```yaml
name: blog                     # required
image: ghcr.io/you/blog        # required, no tag — tags are chosen at deploy time
domain: blog.example.com       # required
port: 8080                     # container port (default 8080)
health: /health                # health path (default /health)
replicas: 1                    # default 1
manual: false                  # true = you own docker-compose.yml
```

Your app must, for zero-downtime deploys:

- **Listen on `port`.** The platform passes it as the `PORT` env var; if your framework reads a different variable (e.g. Spring's `SERVER_PORT`), set that in `.env` to the same value. The health check and Traefik both target `port`, so the app *must* actually listen there.
- **Expose the health endpoint** at `health` and return 2xx when ready.
- **Ship `wget` or `curl` in the image** — the generated health check shells out to one of them. Minimal `scratch`/distroless images have neither; add one (as `examples/go-hello`'s Dockerfile does with `apk add wget`), or take over the compose file with `manual: true` and define your own `HEALTHCHECK`.
- **Handle SIGTERM gracefully** — finish in-flight requests, then exit within 30s.

See `examples/go-hello` for a complete reference app. If you set `manual: true`, your compose file must still define a `healthcheck` — the rolling deploy waits on Docker's health status and will otherwise time out.

## How zero-downtime works

`platform deploy blog abc123` renders the compose file, pulls `image:abc123`, starts new replicas *alongside* the old ones, waits for their health checks to pass, then gracefully stops the old containers (SIGTERM, 30s drain). Traefik discovers replicas through Docker, so traffic shifts automatically and no request is dropped. If the new containers never become healthy, they are removed and the old ones keep serving — a failed deploy changes nothing.

Every deploy is recorded in `apps/<name>/.deployments`, so:

```bash
platform status            # all apps: state, current tag, healthy replicas
platform status blog       # one app + deployment history
platform rollback blog     # redeploy the previous successful tag
platform scale blog 3      # Traefik load-balances across replicas automatically
```

Manage an app's environment (the `.env` file) without a text editor:

```bash
platform env blog list                       # show current keys and values
platform env blog set STRIPE_KEY=sk_live_... # add or update one or more keys
platform env blog unset STRIPE_KEY           # remove keys
platform deploy blog                          # apply — .env is read when containers start
```

Rollbacks work because CI pushes every image twice: as `latest` and as the commit SHA. Keep SHA tags in your registry.

## CI/CD

Fork this repo and copy `workflows/deploy-template.yml` to `.github/workflows/deploy-template.yml` in your fork. On the VPS, create a deploy SSH key; in each app repo add secrets `VPS_HOST`, `VPS_USER`, `VPS_SSH_KEY`. Then each app repo needs only the ~10-line caller workflow (see `examples/go-hello/.github/workflows/deploy.yml`): push to `main` → build → push to GHCR → `platform deploy <app> <sha>` over SSH. The `workflow_dispatch` input lets you deploy any historical commit SHA from the Actions tab.

**GHCR is private by default, so the VPS must be able to pull it.** `platform deploy` runs `docker compose pull`, which fails with `denied`/`unauthorized` on a private package. Either make the package public (repo → Packages → package → *Package settings* → *Change visibility* → Public), or log the VPS in once with a personal access token that has `read:packages`:

```bash
echo "$GHCR_PAT" | docker login ghcr.io -u YOUR_USER --password-stdin
```

The login persists in `~/.docker/config.json`, so every later `platform deploy` can pull. A read-only token is enough — the VPS never pushes.

## Observability

Point your app's OpenTelemetry exporter at `http://openobserve:5080/api/default` (already pre-filled in each app's generated `.env`). Java: attach the OTel javaagent. Go: otelhttp + OTLP HTTP exporter. Python/Node: the standard OTel SDKs. Logs written to stdout are viewable live in Dozzle; ship them to OpenObserve with the OTel SDK or a lightweight collector (Vector/Fluent Bit) if you want search and retention.

## Memory budgeting (4GB VPS)

Infra idles around 1–1.3GB, leaving ~2.5GB for apps. Go/Node/Python services typically take 50–150MB each. Spring Boot is the heavy one: always set `-Xmx256m` (or similar), and remember that during a rolling deploy two copies of an app run simultaneously — budget for the peak, not the steady state.

## Restoring a backup

Dumps land in `infra/backups/`. To restore a single database:

```bash
gunzip -c infra/backups/daily/<file>.sql.gz | docker exec -i fast-infra-postgres-1 psql -U postgres
```

## Design principles

Fewer decisions beat more features. One proxy, one database, one cache, one observability tool, one way to deploy. Everything is a file you can read: no daemon, no hidden state, no API surface to secure — the only way in is SSH. The CLI is a single static binary with zero dependencies that shells out to Docker; you can read all of it in ten minutes.

## Roadmap

- v2 (done): `platform env`, sequential rolling for multi-replica apps, richer status/history
- v3 (only if people actually use this): REST API + web UI on top of the same Go internals

## License

MIT
