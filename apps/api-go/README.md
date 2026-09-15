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

The core Project schema test follows the same pattern:

```bash
PROJECT_TEST_DATABASE_URL=postgres://... go test -count=1 ./internal/project -run TestProjectModelsAgainstDjangoSchema
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

## Migrated module: workspace user preferences

The workspace sidebar preference GET and PATCH routes are implemented with the
seven Django preference keys, default ordering, and pinned-item behavior. Like
Django, seeding goes through a bulk insert that leaves `created_by` null, and
the PATCH route writes only `is_pinned` and `sort_order`.

## Migrated module: workspace home preferences

The workspace home preference GET and PATCH routes seed the `quick_links`,
`recents`, and `my_stickies` widgets with Django's descending sort order,
return the stored `config` on the list route only, and reject writes from
non-members with the `allow_permission` error body.

## Migrated module: issue attachments

The version two routes under `assets/v2/workspaces/<slug>/projects/<id>/issues/<id>/attachments/`: reserve, list, download, mark uploaded, delete. The version one endpoint under `issue-attachments/` stays on Django, because it takes a multipart body and pushes the bytes through the API; the two live at different paths, so the proxy splits them cleanly.

The bytes never pass through the Go API either. The create route reserves a row and returns a policy the browser posts the file against directly, and the download route answers `302` with a signed URL rather than the file. `internal/storage` signs both.

`minio-go` does the signing rather than the AWS SDK, because the SDK has no presigned POST — it signs `GET` and `PUT`, and a browser upload against a policy document is a `POST`. The policy pins the bucket, the key, the content type and a size range starting at one byte, which is what refuses an empty upload.

The filename becomes part of the object key, so `sanitizeFilename` is a port of `plane.utils.path_validator.sanitize_filename` and is checked against that function over a generated corpus of hostile names. The corpus caught a divergence that reading the Python did not: Go's `path.Base` trims trailing separators before taking the last component, so it turns `"trailing/"` into `"trailing"` where Python's `os.path.basename` returns nothing at all. The port takes everything after the last separator instead. Order matters too — whitespace is stripped before the leading dots, so `" .env"` loses both.

The upload callback is `PATCH`. It moves `is_uploaded` and sends the activity only the first time, so a repeated call does not announce the attachment twice, and it deliberately does not reassign `created_by`: that was GHSA-5mxw-g5mw-3v3w. The list shows only rows that finished uploading, so a policy that was never used stays hidden.

Deleting flags the row rather than removing it and leaves the object in the bucket for the storage sweep. `allow_permission([ADMIN], creator=True)` means whoever uploaded an attachment may remove it, and otherwise a project or workspace admin may.

## Migrated module: issue archive

`GET`, `POST` and `DELETE` on `issues/<uuid>/archive/`, and `POST` on `bulk-archive-issues/`. The `archived-issues/` list stays on Django: it needs the grouped paginator and the filter machinery.

An issue may only be archived once its work is over — a state group of `completed` or `cancelled`. Django reads `issue.state.group` with no guard, so a stateless issue raises `AttributeError` and answers `500` rather than the `400` the check would give; that is reproduced.

The three routes read through three different managers, and the difference matters. Archiving goes through `issue_objects`, which hides the archived and draft rows. Unarchiving and the retrieve go through the plain soft-delete manager, which is the only way to reach a row that is already archived. Bulk archiving goes through the plain manager too, so an already-archived or draft issue is included there even though the single-issue route could not reach it.

`Issue.save` writes the whole instance rather than the one column the caller touched, so archiving also recomputes `description_stripped` and moves `updated_at`. `completed_at` stays put: `_sync_completed_at` returns early unless the state itself changed. The bulk route uses `bulk_update`, which writes only `archived_at`, so there no timestamp moves and nothing is recomputed — the two routes genuinely differ.

The bulk route checks each issue inside the loop and returns from it, so a batch containing one unfinished issue leaves the activities already queued for the issues ahead of it queued, while `bulk_update` never runs and nothing is archived. Reproduced rather than tidied: the task is fire-and-forget, so hoisting the check would change which notifications a caller sees.

The retrieve annotates only `is_subscribed`. Every field `IssueDetailSerializer` reads off one of the other annotations is therefore dropped by DRF, leaving the plain instance plus `description_html` and `is_subscribed` — twenty fields, with `is_intake` absent. That count came from rendering the real serializer against an unannotated instance.

## Migrated module: issue relations

`GET` and `POST` on `issues/<uuid>/issue-relation/`, and `POST` on `issues/<uuid>/remove-relation/`.

A relation is stored once and read from whichever end the caller is standing at, so six of the directions a caller may ask for collapse onto three stored types: `blocking` is a `blocked_by` row with the two issues swapped, and the same for `start_after`/`start_before` and `finish_after`/`finish_before`. `duplicate` and `relates_to` are symmetric and stored as asked.

The list therefore answers with eight keys over those stored types. The two symmetric ones are read from both ends inside a single query, because Django unions the two readings and applies `DISTINCT`: querying them separately would list an issue related from both ends twice.

None of it is scoped to the project, only to the workspace. A relation may legitimately cross projects, so the workspace is what stops a cross-tenant reference, and the create route narrows the submitted ids the same way.

The list returns `values()`, so `state_id` is the raw column and is present as null for a stateless issue. The create and delete paths go through the relation serializers instead, whose `state_id` sources from `related_issue.state.id`; DRF's attribute walk stops at the null state and raises `SkipField` on a field that is not required, so there the key is **absent** rather than null. `assignee_ids` is declared `write_only` on both serializers and never reaches a response, and `relation_type` comes back as the stored type, so a request that asked for `blocking` is answered with `blocked_by`.

Two rough edges are reproduced rather than tidied. `remove-relation` calls `.delete()` on the result of `.first()`, so removing a pair that is not there is a `500`, not a `404`. And the create route writes with `ignore_conflicts`, so a pair that already exists is skipped silently while still appearing in the response.

## Shared: DRF datetime rendering

Go's `encoding/json` writes a time as RFC 3339 with trailing zeros trimmed from the fraction, so `03:04:05.120000` comes out as `03:04:05.12Z`. Python's `isoformat` pads the fraction to six digits whenever it is non-zero and omits it entirely when it is zero, so the same instant is `03:04:05.120000Z`. Around a tenth of all timestamps land on a trailing zero, so every migrated route was returning a body that was wrong some of the time and identical the rest of it — a difference no ordinary test notices.

`internal/drf` renders the Django way, and `drf.Respond` is how every success body is written. The rewrite sits at the boundary rather than in each serializer because a serializer that forgets it produces exactly that intermittently-wrong body; `TestEverySuccessResponseGoesThroughRespond` scans the tree and fails the build if a route writes one directly. Error bodies stay on plain `c.JSON`, since they carry no datetimes.

`Respond` walks maps and slices and copies rather than mutating, because the grouped responses serialize one map into several buckets. It does not reach into structs: a struct field is the serializer's own business.

The fixture, `internal/drf/testdata/iso8601.tsv`, comes from DRF itself via `tools/generate_drf_time_fixture.py`, and CI regenerates and diffs it. The generator checks both DRF paths against each other — `DateTimeField.to_representation` for serializer output and the `JSONEncoder` the renderer falls back to for a raw `values()` dict — so one Go type covering both is a checked claim rather than an assumption.

## Shared: editor HTML sanitization

`internal/htmlsanitizer` ports `plane.utils.content_validator.validate_html_content`,
which Django runs over stored editor HTML with nh3, the Python binding for the
Rust ammonia crate. ammonia parses with html5ever, filters the tree, and
serializes it again, so its output carries HTML5 tree construction: unclosed
elements get closed, a bare table gains its `tbody`, and misnested inline
elements are repaired. The port therefore parses with `golang.org/x/net/html`,
which implements the same algorithm, rather than filtering tokens.

Expectations in `sanitizer_test.go` are the output of nh3 0.2.18, the version
pinned in `apps/api/requirements/base.txt`. Output matches nh3 on the editor
markup this policy is meant for; `<svg>` and `<math>` subtrees can serialize
differently because html5ever and `x/net/html` build foreign content
differently. Those elements are in no allowlist and are unwrapped either way,
and `TestCleanNeverEscapesThePolicy` reparses generated markup to assert that
nothing outside the allowlisted tags, attributes, and URL schemes ever survives.

## Migrated module: workspace quick links

The workspace quick link list, create, retrieve, partial-update, and delete
routes are scoped to the requesting user's own links. They keep Django's
scheme-prefixing of bare URLs, its `URLValidator` rules (now shared through
`internal/validate`), the project-derived
workspace resolution in `WorkspaceBaseModel.save`, the soft delete and its
related-object task, and the two different not-found bodies the retrieve and
partial-update routes return. `internal/validate` holds the port of Django 5.2's `URLValidator`, verified
against its output, and the scheme-prefixing both link serializers apply.

## Migrated module: core project

Core Project list, retrieve, create, partial-update, and delete routes are
implemented in `internal/project`. They keep Django's two different list
shapes, the annotated queryset behind `ProjectListSerializer`, the guest and
member visibility narrowing, and the create side effects: the project
identifier row, the acting user and the project lead as project admins, each
admin's `ProjectUserProperty` ordered ahead of their existing projects, and the
six default states inserted the way `bulk_create` does, leaving the slug empty.
`description_html` goes through `internal/htmlsanitizer` the way
`ProjectSerializer.validate` runs it through nh3.

Updating re-derives `intake_view` from the `inbox_view` alias, refuses archived
projects, and creates the default Intake when the view is switched on. Deleting
soft-deletes the project with its deploy boards and favorites and queues the
same Celery tasks. The Celery publisher gained keyword-argument support here,
because `model_activity`, `webhook_activity`, and `recent_visited_task` are all
called with keywords.

`projects/details/`, `project-identifiers/`, invitations, archiving, favorites,
and deploy boards remain on Django.

## Migrated module: project labels

The project label list, create, retrieve, update, delete, and bulk-create routes
are implemented. They keep `Label.save`'s sort ordering, where a new label lands
10000 past the project's current highest, the case-insensitive
`LABEL_NAME_ALREADY_EXISTS` check from `validate_name`, the separate
case-sensitive pre-check the update route runs before it loads the row, the
narrow seven-field serializer shape, and the workspace label cache invalidation
these routes trigger.

## Migrated module: project members

Project member list, add, retrieve, partial-update, remove, and leave routes are
implemented in `internal/project`, together with `project-members/me/`,
`project-views/`, the per-member preference routes, and
`users/me/workspaces/<slug>/project-roles/`.

They keep Django's `allow_permission` project level, which accepts one of the
listed project roles or an active membership plus the workspace admin role, and
the layered checks on role changes and member deactivation. Adding members
reactivates and re-roles existing rows, rejects roles that do not fit the
target's workspace role, seeds each new member's `ProjectUserProperty` ahead of
their existing projects, and queues the same invitation email.

`ProjectMemberRoleSerializer` returns every declared field: the views pass a
`fields` argument, but `DynamicBaseSerializer` overwrites it with `expand`, so
nothing is filtered. Retrieve returns the admin shape only when the caller's
project role is above guest.

## Migrated service: Celery worker, email tasks

`cmd/worker` runs the Go half of the Celery workload. It is a protocol v2
consumer that reads only the queue named by `PACE_WORKER_QUEUE`, so the Python
worker keeps consuming the shared `celery` queue and the two never compete for a
task only one of them can run. The API routes exactly the task names
`worker.MigratedTaskNames()` reports to that queue; leaving `PACE_WORKER_QUEUE`
unset routes everything back to Python, which is the rollback switch.

The eight email tasks are implemented: magic sign-in, forgot password, user
activation and deactivation, the email update code and its confirmation, the
workspace invitation, and the project addition notice. Each renders the same
template Django renders, derives the plain text part with Django's `strip_tags`
rules, which leave entities escaped, and sends a multipart message using the
SMTP settings from `instance_configurations` with the environment as fallback.
The workspace invitation still writes the plain text back onto the invite row
before sending, as Django does.

A task name that reaches the Go queue without a handler is rejected without
requeueing and logged, so a routing mistake surfaces instead of silently
dropping work.

The periodic database cleanups run on Go too: the five `cleanup_task` entries
the beat schedule drives, plus `recent_visited_task`. They touch only
PostgreSQL, keep Django's 500-row batching and its per-batch error isolation,
and read the same retention windows, where the default is used only when the
variable is unset, unparseable, or negative — zero stays a valid window.

```bash
PACE_WORKER_QUEUE=pace-go go run ./cmd/worker
```

The worker's schema test uses the same rollback-only approach:

```bash
WORKER_TEST_DATABASE_URL=postgres://... go test -count=1 ./internal/worker -run TestMaintenanceTasksAgainstDjangoSchema
```

### Relation graph

`soft_delete_related_objects` runs on Go as well. The Django task walks the
model metadata at runtime to find reverse relations and their `on_delete`
behavior, and PostgreSQL cannot stand in for that: Django creates its foreign
keys without `ON DELETE` actions, so the catalog knows the graph but not whether
a relation cascades or nulls. `tools/generate_relation_graph.py` exports that
metadata to `internal/worker/relation_graph.json`, which the worker embeds, and
the workflow regenerates it and fails on any difference so the file cannot drift
away from the models.

Regenerate it from `apps/api` after changing a model:

```bash
python ../api-go/tools/generate_relation_graph.py > ../api-go/internal/worker/relation_graph.json
```

`hard_delete`, the nightly sweep, runs on Go too and reuses the same graph. It
reproduces Django's deletion collector: cascade into the related rows, null the
`SET_NULL` references, then delete. Django's foreign keys carry no `ON DELETE`
action, so a cascade that misses a child row fails on the constraint, which the
schema test relies on to prove the traversal is complete.

`HARD_DELETE_AFTER_DAYS` accepts zero, meaning everything already soft-deleted.
A negative value is refused and logged, which is a deliberate deviation: Django
reads this one with a bare `int()` and no guard, so a negative window would put
the cutoff in the future and hard-delete every soft-deleted row in the instance.

The cascade reproduces one destructive Django behavior worth knowing about:
`BaseModel.save` blanks `created_by` and `updated_by` whenever there is no
current user, and a worker never has one, so every row the cascade touches loses
those columns. Models that stop at `AuditModel`, such as `ProjectIdentifier`,
keep theirs, and the generated graph records which is which.

## Migrated service: Celery beat

`cmd/beat` replaces the Python beat worker. Django sets `beat_scheduler` to
django_celery_beat's `DatabaseScheduler`, so the schedule lives in the
`django_celery_beat_*` tables and can be added to or retimed at runtime. Reading
only the twelve static entries from `celery.py` would silently drop everything
an operator configured through the admin, so the Go beat reads the tables and
syncs the static entries into them on startup exactly as `setup_schedule` does.

Crontab evaluation follows celery's own parser, including the details that
differ from ordinary cron: a step slices the expanded range rather than testing
divisibility, so `5-20/7` is 5, 12 and 19; a range whose end is below its start
wraps around; the day of month and the day of week must both match; and a
literal `7` for day of week is rejected, because celery's parser bounds that
field at 0 to 6 even though django_celery_beat's help text offers "Sunday is 0
or 7". Each crontab row is evaluated in its own stored timezone.

Solar and clocked schedules are not implemented. A row using one is reported as
an error on every reload rather than skipped quietly, since a periodic task
nobody runs is worse than a noisy log.

```bash
PACE_WORKER_QUEUE=pace-go go run ./cmd/beat
```

**The Go beat replaces `beat-worker`; it must not run beside it.** Both evaluate
the same tables, so running both queues every periodic task twice. The compose
service ships commented out for that reason.

The beat's schema test uses the same rollback-only approach:

```bash
BEAT_TEST_DATABASE_URL=postgres://... go test -count=1 ./internal/beat -run TestBeatStoreAgainstDjangoSchema
```

## Shared: SSRF-safe outbound requests

`internal/httpsafe` ports `plane.utils.ip_address` and
`plane.utils.url_security`, the guard every outbound request on a user-supplied
URL goes through. The rule is not merely "reject private IPs": the host is
resolved, every returned address is checked, and the connection is then made to
the validated IP literal so DNS cannot be rebound between the check and the
connect. IPv4 addresses embedded in IPv6 transition formats are decoded and
checked too, because that embedded address is what the packet reaches. Redirects
are never followed.

The classification tables are CPython's own, read out of
`ipaddress.IPv4Address._constants` and `IPv6Address._constants`. They are easy
to get wrong by hand: an earlier draft used a narrower IPv6 reserved list and
would have allowed 791 of the 3298 addresses in the first cross-check corpus
that Python blocks. The port now matches `is_blocked_ip` on all 67288 addresses
checked, including dense sweeps either side of every boundary.

## Migrated module: issue reactions and subscribers

The issue reaction list, create and delete routes, the issue subscriber routes,
and the `subscribe/` endpoint are implemented. They queue `issue_activity` the
way Django does; that task still runs on the Python worker, and the publisher
routes anything unmigrated to the Celery queue, so no cutover is needed for it.

Two Django behaviors are reproduced rather than corrected. The subscriber list
route does not list subscribers: it returns the project's members through
`ProjectMemberLiteSerializer`, whose `is_subscribed` comes back null because the
annotation feeding it is never added on that route. And `subscribe/` uses
`ProjectLitePermission`, so any active project member may subscribe themselves
whatever their role, while the `issue-subscribers/` routes use
`ProjectEntityPermission` and require admin or member for writes.

## Migrated module: issue links

The issue link list, create, retrieve, partial-update and delete routes are
implemented. They share the URL handling with workspace quick links through
`internal/validate`, queue `crawl_work_item_link_title` so the Python worker
still fetches the page title, and record the same `link.activity.*` entries with
the request body and the pre-change snapshot Django sends. Only a URL that
actually changed is crawled again.

## Migrated module: issue comments and comment reactions

The issue comment routes and the comment reaction routes are implemented. This
is the first caller of `internal/htmlsanitizer`: `IssueCommentSerializer.validate`
runs `comment_html` through nh3 and stores what comes back, and the Go side does
the same through the ported cleaner.

`IssueComment.save` does more than write the row. It derives `comment_stripped`
with Django's `strip_tags`, creates a `Description` row on first save, and on
later saves updates only the description columns whose comment counterpart
actually changed. All three are reproduced, as is the rule that `edited_at` is
stamped only when the submitted html differs from what is stored.

The create route refuses a guest unless the project opens all features to guests
or the guest raised the issue, and update and delete follow
`allow_permission(creator=True)`, so the comment's author may always act on it
and otherwise a project or workspace admin may.

## Migrated module: issue detail

`GET`, `PATCH` and `DELETE` on `issues/<uuid>/` are implemented, with the
annotated queryset behind `IssueDetailSerializer`: the cycle, the link,
attachment and sub-issue counts, and the label, assignee and module id arrays.

Two shape details are reproduced rather than tidied. `is_intake` is declared on
the serializer but only annotated by `IssueDetailIdentifierEndpoint`; DRF makes
a read-only field not required, so `get_attribute` raises `SkipField` and the
key is dropped, which means these routes never return it. And `current_instance`
on the update path comes from a queryset that does not annotate `is_subscribed`,
so that key is absent from the snapshot the activity carries even though the
retrieve response has it.

`PATCH` answers `204` with no body. It narrows assignees to project members at
member level or above and labels to the project, silently dropping the rest
rather than refusing, and skips every activity when `skip_activity` accompanies
a description update. The list route stays on Django: it needs the grouped
paginator and the filter machinery.

## Migrated module: sub-issues

`GET` and `POST` on `issues/<uuid>/sub-issues/` are implemented. Both shapes come from the SQL Django actually renders rather than from reading the view, because several of its filters are not where they look like they are.

`Issue.issue_objects` hides more than soft-deleted rows: it also excludes triage issues, archived issues, draft issues, and issues whose project is archived. An issue with no state passes the triage check, since Django's `exclude()` over a nullable join renders as `NOT (group = 'triage' AND group IS NOT NULL)`. `internal/project.issueObjectsPredicate` is that whole manager, and `sub_issues_count` applies it too.

The annotation joins deliberately keep two gaps. Neither the `project_members` join behind `assignee_ids` nor the `modules` join behind `module_ids` filters `deleted_at`, because a model's default manager only filters that model's own queryset, never a join traversed through it. A soft-deleted project membership therefore still keeps its assignee in `assignee_ids`, exactly as on Django.

The two responses are different shapes. The read route returns `values()`, which is `IssueSerializer`'s twenty-five fields plus `state_group`, with `created_at` and `updated_at` moved into the caller's timezone and nothing else converted. The assign route serializes plain model instances through the same serializer, and none of the seven annotated fields exist on an instance; DRF makes a read-only field not required, so `get_attribute` raises `SkipField` for each and the key is dropped rather than rendered as null, leaving eighteen.

`group_by=assignees__ids` fans an issue out across each of its assignees and files an unassigned one under the literal string `None`. Any other `group_by` is looked up straight in the `values()` dict, so a name that is not one of its keys raises `KeyError`, which `BaseAPIView.handle_exception` turns into a `400` rather than a `500`.

`internal/project/issue_ordering.go` is `order_issue_queryset`. Its fixture, `testdata/issue_order_by.tsv`, is the `ORDER BY` Django renders for each of the fourteen allowlisted fields in both directions, extracted from Django rather than written by hand — which is what caught three divergences that reading the Python did not. No clause pins a `NULLS` position, because Django emits a bare `ASC`/`DESC` and leaves Postgres to apply its defaults. The priority `Case` has no default, so a priority outside the list sorts as null. And Django orders by that `Case` ascending in *both* directions — only the string handed back to the paginator flips — so `priority` and `-priority` return the same order, which is reproduced rather than corrected. State group is the one that really does reverse, by reversing the list the `Case` is built from.

The assign route scopes both the parent lookup and the sub-issue ids to the URL workspace and project, and fires `issue_activity` only for the ids that were really re-parented, so a foreign id cannot reach the task and bump `updated_at` on an issue the caller cannot see.
