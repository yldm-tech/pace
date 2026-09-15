# Pace Go API

This directory contains the incremental Gin, GORM, and PostgreSQL replacement for the Django API. Business modules are migrated in separate pull requests; Django remains the behavioral reference until a module passes its contract and integration tests.

## Run locally

Set `DATABASE_URL`, `SECRET_KEY`, and `REDIS_URL` to the same values used by the Django deployment, then run:

```bash
go run ./cmd/api
```

The server listens on `:8000` by default. Override it with `PACE_API_ADDRESS`.

Authentication publishes the existing Django Celery email tasks through RabbitMQ. Configure `AMQP_URL`, or the same `RABBITMQ_HOST`, `RABBITMQ_PORT`, `RABBITMQ_USER`, `RABBITMQ_PASSWORD`, and `RABBITMQ_VHOST` values used by Django. OAuth, SMTP, signup, and sync flags continue to come from `instance_configurations` when `SKIP_ENV_VAR=1`; encrypted values use the existing Django Fernet format and `SECRET_KEY`.

## Verify

```bash
go test -count=1 ./...
go test -race -count=1 ./...
go vet ./...
```

To verify GORM writes against a disposable PostgreSQL database that already has
the Django schema, run the rollback-only integration test:

```bash
AUTH_TEST_DATABASE_URL=postgres://... go test -count=1 ./internal/auth -run TestGORMRepositoryAgainstDjangoSchema
```

The user/profile/account module uses the same rollback-only shared-schema
verification:

```bash
USER_TEST_DATABASE_URL=postgres://... go test -count=1 ./internal/user -run TestUserModelsAgainstDjangoSchema
```

The core Workspace schema test uses the same rollback-only approach:

```bash
WORKSPACE_TEST_DATABASE_URL=postgres://... go test -count=1 ./internal/workspace -run TestWorkspaceModelsAgainstDjangoSchema
```

Health endpoints are `/api/health`, `/api/health/db`, and `/ready`.

## Migrated module: authentication

The `/auth/` module preserves all 37 Django routes for app and Space authentication:

- credential and magic-code sign-in/sign-up;
- Google, GitHub, GitLab, and Gitea OAuth initiation/callbacks;
- email checks, sign-out, forgot/reset/change/set password, and CSRF tokens;
- shared rate limiting, accepted-invitation processing, and existing Celery email side effects.

Sessions are stored in the existing `sessions` table and encoded with Django's signing format. Passwords, password-reset tokens, CSRF masks, cookies, error codes, and redirects remain compatible during gradual traffic cutover. The Go service does not run schema migrations; Django migrations remain authoritative until the database module is migrated.

## Migrated module: user/profile/account

Core `/api/users/me/` profile, session, settings, profile, OAuth accounts, email
verification, instance-admin, onboarding, deactivation, and `/api/v1/users/me/`
routes are implemented in `internal/user`; the proxy cuts over only these paths
while workspace activity and other API modules remain on Django.

## Migrated module: core workspace

Core workspace CRUD, membership, member preferences, and invitation routes are
implemented in `internal/workspace`. They preserve Django session authentication,
role checks, soft-delete behavior, cache invalidation, Celery workspace seed and
invitation tasks, and the existing PostgreSQL tables. Shared-schema integration
tests run against the authoritative Django migrations, and the proxy sends only
the migrated core routes to Go. Workspace metadata, preferences, favorites,
drafts, activity, and dashboard routes remain on Django for later module-specific
pull requests.

## Migrated module: workspace themes

Workspace Theme list, create, retrieve, partial-update, and soft-delete routes
are implemented separately from the core Workspace module. The module retains
Django's admin/member permission boundary, JSON response shape, audit fields,
name uniqueness, and related-object soft-delete task behavior.

## Migrated module: workspace user properties

The workspace user properties GET and PATCH routes are implemented for the
current user's filters, display settings, rich filters, and navigation settings.

## Module in progress: workspace user preferences

The workspace sidebar preference GET and PATCH routes are being migrated with
the seven Django preference keys, default ordering, and pinned-item behavior.
