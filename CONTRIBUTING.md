# Contributing

## Prerequisites

- Go 1.24+
- Node 22+ for `website/`
- Docker if you want to run the Postgres-backed test job locally

## Local Workflow

Run the core checks before opening a change:

```bash
go test ./...
go test -race ./...
go vet ./...
```

Docs:

```bash
cd website
npm ci
npm run build
```

## Postgres-Backed Tests

Some store coverage runs against a real Postgres instance. The suite reads:

```bash
export MOLTNET_TEST_POSTGRES_DSN='postgres://postgres:postgres@127.0.0.1:5432/moltnet?sslmode=disable'
```

One local setup:

```bash
docker run --rm -d \
  --name moltnet-postgres \
  -e POSTGRES_DB=moltnet \
  -e POSTGRES_USER=postgres \
  -e POSTGRES_PASSWORD=postgres \
  -p 5432:5432 \
  postgres:16-alpine
```

Then run:

```bash
go test ./internal/store
```

## Config Secrets

- `Moltnet`, `MoltnetNode`, and bridge config files that contain tokens must be private (`0600` or equivalent).
- Environment-only secrets such as `MOLTNET_PAIRINGS_JSON` are supported, but they do not get filesystem permission hardening.

## Docs And Guides

- Keep `README.md` focused on operator onboarding.
- Put detailed API and protocol material in `website/src/content/docs/`.
- Keep package `CLAUDE.md` guides in present tense and aligned with current code.
