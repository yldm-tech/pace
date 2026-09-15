# Pace Go API

This directory contains the incremental Gin, GORM, and PostgreSQL replacement for the Django API. Business modules are migrated in separate pull requests; Django remains the behavioral reference until a module passes its contract and integration tests.

## Run locally

Set `DATABASE_URL` to a PostgreSQL connection string, then run:

```bash
go run ./cmd/api
```

The server listens on `:8000` by default. Override it with `PACE_API_ADDRESS`.

## Verify

```bash
go test -count=1 ./...
go vet ./...
```

Health endpoints are `/api/health`, `/api/health/db`, and `/ready`. The initial foundation PR intentionally contains no business-domain routes.
