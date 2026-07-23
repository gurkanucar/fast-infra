# Optional DB/cache provisioning in `platform new`

Date: 2026-07-23
Status: approved

## Problem

`platform new <name>` writes an `apps/<name>/.env` with placeholder connection
strings (`DATABASE_URL=…:CHANGE_ME@…/<name>`, `REDIS_URL=redis://redis:6379`).
The Postgres database is not created and the password is a placeholder the user
must fill in by hand, and there is no per-app Redis isolation. This is a small
but repetitive manual step for every app that needs Postgres or scoped Redis.

## Goal

Let `platform new` optionally provision, per app:
- a dedicated Postgres database **and** a least-privilege role that owns only
  that database, and
- a Redis ACL user scoped to the `<name>:*` key/channel prefix,

writing the real credentials into the app's `.env`. Strictly opt-in.

## Non-goals

- No changes to `remove`/`deploy`: provisioning never drops or mutates existing
  data. `remove` still leaves the database and images intact.
- No global Redis auth: the `default` user stays open so existing apps that use
  `redis://redis:6379` keep working. Per-app users are additive isolation.

## Behavior

Two yes/no prompts at the end of `platform new`, both defaulting to **No** so
the current flow is unchanged unless the user opts in:

```
Create a dedicated Postgres database + user? [y/N]
Create a Redis user scoped to <name>:*?     [y/N]
```

Provisioning is **best-effort**: if the infra containers are not running or a
command fails, `platform new` still creates the app scaffold (app.yaml, .env,
compose), leaves the affected line as the `CHANGE_ME` template, prints a warning
plus the manual command, and exits 0.

## Postgres provisioning

Shell out to `docker exec -i fast-infra-postgres-1 psql -U postgres`.

- Generate a random hex password (no SQL-escaping hazards).
- Create the role if absent, via a `DO` block (idempotent):
  `CREATE ROLE "<name>" LOGIN PASSWORD '<pw>'`.
- Create the database if absent: `createdb -U postgres -O <name> <name>`
  (guarded by a `SELECT 1 FROM pg_database WHERE datname='<name>'` check).
- Least privilege: the role owns only its own database, is not a superuser, and
  cannot modify other apps' objects.
- `.env`: `DATABASE_URL=postgres://<name>:<pw>@postgres:5432/<name>`.

## Redis provisioning

Requires the redis service to load an ACL file so users survive a restart.

Infra change (`infra/docker-compose.yml`):

```yaml
redis:
  command: sh -c 'touch /data/users.acl && exec redis-server --maxmemory 256mb --maxmemory-policy allkeys-lru --aclfile /data/users.acl'
  volumes:
    - redis_data:/data
```

`touch` guarantees the ACL file exists before redis loads it; `exec` keeps redis
as PID 1 for signal handling; an empty file leaves the `default` user open.
Add `redis_data` to the top-level `volumes:`. Recreating redis flushes its
in-memory cache (no persistence is configured) — acceptable for an LRU cache.

Then, per app, shell out to `docker exec fast-infra-redis-1 redis-cli`:

- `ACL SETUSER <name> on ><pw> ~<name>:* &<name>:* +@all`
- `ACL SAVE` (persists to `/data/users.acl`).
- `.env`: `REDIS_URL=redis://<name>:<pw>@redis:6379`.

## Code

- New `cli/provision.go`, stdlib only, shelling out to `docker`:
  - `provisionPostgres(name string) (password string, err error)`
  - `provisionRedis(name string) (password string, err error)`
  - Pure helpers that build the SQL and the `ACL SETUSER` argument list, kept
    separate so they can be unit-tested.
- `cli/new.go`: after the existing prompts, ask the two opt-in questions; call
  the provisioners on yes; build `.env` from the results (real creds when
  provisioned, `CHANGE_ME`/plain template otherwise). Extract the `.env` body
  into a pure `renderAppEnv(name string, db, redis *conn) string` helper.

## Testing

- Unit tests (no Docker): `renderAppEnv` for all four provisioned/not
  combinations; the SQL role/db command builders; the `ACL SETUSER` arg builder
  for the prefix scoping.
- Live verification on the VPS: opt in for a throwaway app, confirm the DB +
  role exist and the role is non-superuser, the Redis user is scoped (a key
  outside `<name>:*` is denied), the app's `.env` has working creds, and a
  redis restart keeps the ACL user.

## Docs & principle

- README "Creating and deploying an app": document the optional prompts.
- CLAUDE.md: note `provision.go`, the redis aclfile change, the least-privilege
  model, and that provisioning is opt-in and create-only.
- Reconciliation with "the platform never touches data": default is unchanged
  (press Enter), provisioning only ever creates, never drops or mutates. So the
  zero-decision path is preserved.
