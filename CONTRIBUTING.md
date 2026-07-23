# Contributing

fast-infra is deliberately small. Before proposing a feature, read the design
principle in [CLAUDE.md](CLAUDE.md): **fewer decisions beat more features**. A change
that adds a decision to the install → `platform new` → `git push` path has to earn it.

## Build and test

The CLI is stdlib-only Go — no external dependencies, and it should stay that way.

```bash
cd cli
go build -o platform .   # build
go vet ./...             # vet
go test ./...            # unit tests — no Docker needed
```

After an intentional change to the compose template, regenerate the golden file:

```bash
cd cli && UPDATE_GOLDEN=1 go test -run TestRenderGolden .
```

Keep `install.sh` `shellcheck`-clean.

## Conventions

- Commit messages: plain imperative English, no AI attribution or Co-Authored-By trailers.
- New CLI commands: one file per command, add the `case` to the switch in `cli/main.go`, stdlib only.
- `README.md` stays prose-first and short; architecture and rationale live in `CLAUDE.md`.
- Shell scripts: `set -euo pipefail`, LF line endings.

See [CLAUDE.md](CLAUDE.md) for the architecture, the app contract, and the roadmap
(no v3 daemon / API / web UI during v1/v2).
