# Agent Development Guide

## Commands

- `pnpm dev` - Start all dev servers (web:3000, admin:3001)
- `pnpm build` - Build all packages and apps
- `pnpm check` - Run all checks (format, lint, types)
- `pnpm check:lint` - OxLint across all packages
- `pnpm check:types` - TypeScript type checking
- `pnpm fix` - Auto-fix format and lint issues
- `pnpm turbo run <command> --filter=<package>` - Target specific package/app
- `pnpm --filter=@pace/ui storybook` - Start Storybook on port 6006

## Code Style

- **Imports**: Use `workspace:*` for internal packages, `catalog:` for external deps
- **TypeScript**: Strict mode enabled, all files must be typed
- **Formatting**: oxfmt, run `pnpm fix:format`
- **Linting**: OxLint with shared `.oxlintrc.json` config
- **Naming**: camelCase for variables/functions, PascalCase for components/types
- **Error Handling**: Use try-catch with proper error types, log errors appropriately
- **State Management**: MobX stores in `packages/shared-state`, reactive patterns
- **Testing**: All features require unit tests, use existing test framework per package
- **Components**: Build in `@pace/ui` with Storybook for isolated development

## Backend tests (Docker)

The Go suite for `apps/api` runs against real services in an isolated stack defined by `docker-compose-test.yml` at the repo root. The schema is built by the Go migrator before the tests run, so a broken migration fails before anything else gets the chance.

Prereq (once): `./setup.sh` — generates `apps/api/.env`.

- Full suite: `docker compose -f docker-compose-test.yml up --build --abort-on-container-exit --exit-code-from api-tests`
- Subset: `docker compose -f docker-compose-test.yml run --rm api-tests go test ./internal/beat/ -run Schema`
- Teardown: `docker compose -f docker-compose-test.yml down -v`

Most of the suite needs nothing but Go. The parts that want a database are opt-in on one environment variable each, and the stack sets all of them; running `go test ./...` without them skips those rather than failing.
