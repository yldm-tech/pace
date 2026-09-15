# Pace

Pace is an open-source project management platform for tracking work items, running cycles, and managing product roadmaps without the overhead of managing the tool itself.

This repository is the Pace application and its gradual backend migration. The web application remains compatible with the existing product while API modules move from Django to Go (Gin + GORM) against the same PostgreSQL schema.

## Features

- **Work items** — create, organize, and track tasks with rich text, files, and relationships.
- **Cycles** — plan iterations and monitor progress with cycle views and burn-down charts.
- **Modules** — break large projects into smaller, manageable units.
- **Views** — save and share filters for the issues that matter most.
- **Pages** — capture ideas and turn notes into actionable work.
- **Analytics** — understand project health and remove blockers with real-time insights.

## Installation

See [CONTRIBUTING.md](./CONTRIBUTING.md) for local development instructions. The existing Docker and Kubernetes deployment configuration is still supported while the migration is in progress.

For the Go API, see [`apps/api-go/README.md`](./apps/api-go/README.md).

## Built with

[![React Router](https://img.shields.io/badge/-React%20Router-CA4245?logo=react-router&style=for-the-badge&logoColor=white)](https://reactrouter.com/)
[![Django](https://img.shields.io/badge/Django-092E20?style=for-the-badge&logo=django&logoColor=green)](https://www.djangoproject.com/)
[![Go](https://img.shields.io/badge/Go-00ADD8?style=for-the-badge&logo=go&logoColor=white)](https://go.dev/)
[![Gin](https://img.shields.io/badge/Gin-008ECF?style=for-the-badge&logo=gin&logoColor=white)](https://gin-gonic.com/)
[![GORM](https://img.shields.io/badge/GORM-1F6FEB?style=for-the-badge&logo=go&logoColor=white)](https://gorm.io/)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL-4169E1?style=for-the-badge&logo=postgresql&logoColor=white)](https://www.postgresql.org/)
[![Node JS](https://img.shields.io/badge/node.js-339933?style=for-the-badge&logo=Node.js&logoColor=white)](https://nodejs.org/en)

## Backend migration status

The migration is intentionally incremental. Django remains the behavioral reference and continues to serve every module that has not passed contract and integration verification in Go.

| Stage                                                                                   | Scope                                                   | Status      |
| --------------------------------------------------------------------------------------- | ------------------------------------------------------- | ----------- |
| [PR #1](https://github.com/yldm-tech/pace/pull/1)                                       | Pace brand update                                       | Complete    |
| [PR #2](https://github.com/yldm-tech/pace/pull/2)                                       | Go API foundation (Gin, GORM, PostgreSQL, proxy wiring) | Complete    |
| [PR #3](https://github.com/yldm-tech/pace/pull/3)                                       | Authentication (`/auth/`)                               | Complete    |
| [PR #4](https://github.com/yldm-tech/pace/pull/4)                                       | User, profile, and account APIs                         | Complete    |
| [`feat/go-api-workspace`](https://github.com/yldm-tech/pace/tree/feat/go-api-workspace) | Workspace APIs                                          | In progress |

Migration rules:

1. One business module gets one independent branch and pull request.
2. Django behavior is the compatibility baseline for URLs, status codes, JSON, permissions, sessions, cookies, and database side effects.
3. A module is cut over only after contract and integration tests pass against the existing Django schema.
4. Unmigrated routes continue to be served by Django; migration work does not require a flag day.

### Verify the Go API

```bash
cd apps/api-go
go test -count=1 ./...
go test -race -count=1 ./...
go vet ./...
```

To verify GORM writes against a disposable PostgreSQL database that already has the Django schema, opt in with a connection string:

```bash
AUTH_TEST_DATABASE_URL=postgres://... \
  go test -count=1 ./internal/auth \
  -run TestGORMRepositoryAgainstDjangoSchema
```

## Community and contributing

Open an issue or pull request in the [Pace repository](https://github.com/yldm-tech/pace). Please read [CONTRIBUTING.md](./CONTRIBUTING.md) before submitting changes. Security issues should be reported privately to the maintainers rather than disclosed in a public issue.

## License

This project is licensed under the [GNU Affero General Public License v3.0](./LICENSE.txt).
