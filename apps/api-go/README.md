# Pace Go API

This directory contains the incremental Gin, GORM, and PostgreSQL replacement for the Django API. Business modules are migrated in separate pull requests; Django remains the behavioral reference until a module passes its contract and integration tests.


## Run locally

Set `DATABASE_URL`, `SECRET_KEY`, and `REDIS_URL` to the same values used by the Django deployment, then run:

```bash
go run ./cmd/api
```

The server listens on `:8000` by default. Override it with `PACE_API_ADDRESS`.

Authentication publishes the existing Django Celery email tasks through RabbitMQ. Configure `AMQP_URL`, or the same `RABBITMQ_HOST`, `RABBITMQ_PORT`, `RABBITMQ_USER`, `RABBITMQ_PASSWORD`, and `RABBITMQ_VHOST` values used by Django. OAuth, SMTP, signup, and sync flags continue to come from `instance_configurations` when `SKIP_ENV_VAR=1`; encrypted values use the existing Django Fernet format and `SECRET_KEY`.

## The cutover guard

The proxy cuts traffic over by **path**, not by method. A matcher covering a path Django serves with four methods sends all four to Go, and any method the Go router does not register becomes a 404 the moment that matcher merges.

That happened. `cycle-issues/` was cut over with only its write half implemented, so the cycle board's issue list 404'd for as long as it took to notice; and the same mistake had put `POST issues/` — creating an issue — behind a matcher that only served `GET`.

`TestEveryCutOverPathIsFullyServed` compares three sources directly: every route Django serves, the paths the Caddyfile cuts over, and the routes the Go router registers. A path that is cut over must have **every** one of Django's methods. It found seven gaps the first time it ran.

The Django side comes from `tools/generate_django_routes.py`, which walks the real URLconf. A viewset records its method mapping, so that one is exact; a plain `APIView` has none, so every handler it defines is reachable on every path bound to it — except one whose signature does not match the path's captured parameters, which raises before it does anything. Those are compared and left out, which is what took the first run from thirteen reported gaps down to seven real ones.

CI regenerates the inventory and diffs it, so a route added to Django shows up here rather than in production.

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

## Migrated module: the issue list, grouped

`GET` on `issues/` with `group_by`. The rows come out of a window partitioned by the group, so one query returns a page of **every** group at once rather than a page of the whole set, and whether a page follows is answered by asking whether any row sits past the window rather than by counting.

The per-group totals are the part worth naming. `count_filter` is an **aggregate** filter, not a predicate: as a predicate a group whose every issue is excluded would disappear, where as an aggregate filter it still appears with a count of zero — which Django then records as **one**. Writing it as a `WHERE` would have quietly dropped those groups.

## Migrated module: the issue list, sub-grouped

`GET` on `issues/` with both `group_by` and `sub_group_by`. The window partitions by both axes at once, so one query still returns a page of every group and sub-group, and either axis may bring its own join — an issue can be fanned by both.

`internal/pagination.SubGroupRows` is the nesting itself, diffed against the real class over a corpus that deliberately includes the cases where it **raises**. That is half of what the fixture is for: the plain path indexes the pre-seeded dictionary without checking, so a row whose group and sub-group pair was not seeded answers `500`, while the many-to-many path checks first and drops the row silently. Nothing in the Python says so.

Only sub-groups that actually have a total are pre-seeded, so a group can come back with an empty results map even though its own total is set. And the zero-counts-as-one rule applies to the group totals only — a zero in the sub-group totals stays zero.

With this the whole route is migrated, and the proxy now sends `issues/` to Go.

## Shared: the grouped issue list's query machinery

`internal/project/issue_list_grouped.go` holds what the grouped paths need before the handler can assemble them: the group-by allowlist, the expression each name partitions by, the join the three many-to-many ones require, the ordering inside the window, where each group-by's known values come from, and the filter the per-group totals carry.

The allowlist is load-bearing. The name reaches `F()`, `values()`, `order_by()` and a window's `partition_by`, and it was added after the fact (GHSA-wwgj-929g-42cm), so the eleven entries are pinned by a test — as is the rule that every allowed name has both a partition expression and a source of group values, so no caller can reach a nil one.

The three many-to-many group-bys join with a **LEFT OUTER**, which is what lets an issue with no labels partition under the null group rather than vanish. That was taken from the SQL Django renders for the window, not from the field names.

The window's ordering is not the list's, in two ways. It spells `NULLS LAST` explicitly where the plain queryset leaves it to Postgres. And it orders by the string `order_issue_queryset` hands back rather than the one the caller sent — which for priority is the **opposite** direction, since that function returns the inverted annotation name. So `?order_by=priority` orders the list one way and the window the other, and both are reproduced.

Three group-bys append the literal `None` so an issue in no group still gets a bucket; the rest do not. And the project group list is workspace-wide even when a project is named, which is the one place the scoping is not applied.

## Migrated module: the cycle issue list

`GET` on `cycles/<uuid>/cycle-issues/`, flat and grouped. It shares the project list's filtering, ordering, grouping and paging, narrowing the issue_objects manager with a single EXISTS that carries both halves of the link condition — Django puts them in one filter call, so they apply to the same joined row.

It carries neither the guest narrowing nor the recorded visit the project list does.

## Migrated module: cycle issues and the archive toggle

Adding and removing a cycle's issues, and archiving or unarchiving the cycle itself.

An issue belongs to at most one cycle, so adding one that is already in another **moves** it rather than duplicating it. Both the existing links and the ids that will get new ones are scoped to the workspace and project, which is what stops a foreign link from being reassigned into this cycle (GHSA-4w5x-wc9w-f47x).

The activity the add sends is the awkward part. `created_cycle_issues` is a **JSON string** inside the snapshot rather than a nested object, because Django builds it with `serializers.serialize` and then dumps the whole snapshot around it — and the task calls `json.loads` on it, so the nesting has to survive. The task reads only `fields.cycle` and `fields.issue` out of each record, but the shape it reads them through is the full serialize format.

Removing an issue sends its activity **before** the link goes, since the task reads the cycle by the id the request named rather than from the link. The delete is a queryset delete, so it answers `204` whether or not the link was there.

Only a **completed** cycle may be archived — one with no end date, or one whose end date has not passed, is refused. Archiving clears **every member's** favourite, where deleting the cycle clears only the caller's.

## Migrated module: cycle create, read, update and delete

The four routes under `cycles/<uuid>/`, plus `POST` on `cycles/`.

The two dates travel together: either both are given or neither is, and one alone is refused. When both are given they go through `convert_to_utc` as a pair, anchored to the project's own day; when they are not, neither is written at all. That is what the write serializer's `validate` does, and it is why a request supplying only an end date changes nothing rather than half of something.

An **archived** cycle cannot be updated at all. A **completed** one — one whose end date has passed — can only be reordered, and a request that also carries other fields is narrowed to the sort order rather than refused, which is what lets a board be rearranged after a cycle closes.

The three write responses drop `cancelled_issues`, which only the list carries, and the retrieve adds `sub_issues`, which only it carries. The update's snapshot is `CycleSerializer` over the **instance**, so it carries none of the annotated counts.

Deleting needs a project admin or whoever created the cycle. The favourite is soft deleted along with it and the recent visit is removed outright, and the activity carries the cycle's id in the issue slot, which is how the task finds it.

## Migrated module: the cycle list

`GET` on `cycles/`, which returns every cycle of a project that is not archived.

Its dates are rendered in the **project's** timezone rather than the caller's. That is the one place in the codebase the distinction is made, and it is why `timeIn` exists alongside the caller-timezone conversion the issue routes use.

The three counts each carry the same four exclusions — the cycle link and the issue must both be live, and the issue must be neither archived nor a draft — so a cycle's totals agree with what its board shows. The assignee aggregate deliberately carries **none** of them: a soft-deleted cycle link still contributes its assignees, which is what the queryset does. The filter on the assignee link itself sits on the same join as the value being aggregated, checked against the rendered SQL rather than assumed.

`status` is derived rather than stored, from where today sits relative to the two dates. A cycle with a start date but no end date falls through every branch to `DRAFT`.

`cycle_view=current` narrows the list to what is running, and falls back to the whole list when nothing is. The list orders favourites first and then newest, overriding the queryset's own ordering by name.

## Migrated cycle: the analytics endpoint and the burndown chart

`GET` on `cycles/<uuid>/analytics/`, which is the cycle board's chart: the work spread across people and labels, and a day-by-day burndown.

`type=points` against a project whose estimate is not in points is **not** an error. It returns two empty lists and an empty chart, because the branch that fills them is simply not entered. A cycle with no start or end date answers `400`, and a cycle that does not exist answers `500` — Django reads the start date off the result of `.first()` with no guard.

A cycle carrying a progress snapshot returns the distribution stored in it and runs none of the live queries, key by key: a missing `labels` defaults to `[]` and a missing `completion_chart` to `{}`.

### Counting a null column counts nothing

Each issue count is `Count("assignee_id", ...)` — over the **grouping column**, not over the row. So the bucket holding the issues with no assignee reports `0` total issues, not the number of them. Same for the unlabelled bucket. That is upstream's behaviour and the frontend draws it.

The point sums have the matching quirk in the other direction: a sum with nothing to add is `null`, and nothing rewrites it, so a bucket whose issues are all still open reports `null` completed points rather than zero.

The assignee and label joins do not filter `deleted_at`, because a many-to-many traversal joins the through table plainly. A soft-deleted assignment still appears in the distribution. This is the same gap the `assignee_ids` annotation has.

### The burndown counts down from a total the distributions do not agree with

`total_issues` for the chart is a filtered `Count` written on the **cycle** queryset, so it excludes deleted, archived and draft issues and dead links — and it does **not** exclude a triage issue or one in an archived project. The distributions drawn beside it go through the `issue_objects` manager, which does. The two can disagree about the same cycle, and both are reproduced.

The points plot subtracts one row **per estimated issue** rather than one per day: the Python never groups them, and it sums the whole list again for every day of the window. A row whose issue was never completed has a null day and is skipped.

`TruncDate` is written as `AT TIME ZONE 'UTC'` rather than left to Postgres' `DATE()`, which reads the connection's own `TimeZone`. Django always names the zone it is configured with, and the two do not have to agree.

### Python's integer zero reaches the chart

`sum([])` is the integer `0`, not `0.0`. So a points chart for a cycle holding no estimated issue at all is full of `0`, while one holding a single estimated issue is full of `0.0` — an integer minus an integer stays an integer all the way into the body. The fixture covers both, and the chart is compared as rendered text rather than as parsed numbers, which is the only way the difference is visible.

## Migrated cycle: the progress endpoint

`GET` on `cycles/<uuid>/progress/`.

The two halves of the body do not come from the same place. The **point sums are always live**; the **issue counts are read from the cycle's progress snapshot** whenever it has one. A cycle whose issues were transferred away keeps the numbers it had at the moment of transfer, because the live count would report the empty cycle it has become. A missing key in the snapshot defaults to zero rather than falling back to counting.

### Zero is spelled two ways in the same body

Five of the six point sums go through Python's `or 0`, so an absent sum **and a sum that is exactly zero** both come back as the integer `0`. The sixth, `total_estimate_points`, carries a Django `default=` instead, which becomes `Coalesce(..., 0)` over a float column — so it is `0.0`. Two fields, four characters apart, in the same response.

The fixture walks every shape the aggregate can produce, including `-0.0`, where the two rules diverge most clearly: `or 0` gives `0` and the coalesced total gives `-0.0`. It renders through the real response path rather than through a stand-in, so it covers the float rewriting too.

The five grouped sums count a non-matching issue as **zero rather than skipping it** — the `Case` has an `ELSE 0` — so they are non-null as soon as the cycle holds a single estimated issue, whatever state it is in. Only a cycle with no estimated issues at all produces the null.

## Migrated cycle: the archived cycle list

`GET` on `archived-cycles/`.

Written straight after its module counterpart, and the two differ in both of the ways the two apps tend to differ.

**A guest may read archived modules and may not read archived cycles.** This endpoint is decorated for admins and members; the module one leans on the permission class, which lets any active member through a safe method.

**This queryset keeps the membership and project-archived filters that the live cycle list has.** The module endpoint drops them, so an archived project's archived modules still appear while its archived cycles do not.

The projection is twenty-three fields where the live list has twenty-two, and it is not a superset: no `logo_props`, `version` or `created_by`, plus the three state counts and `archived_at`. Two lists of nearly equal length that are not the same list is the worst kind of difference to carry, so the test asserts it from both sides.

Nothing here moves timezone. The live list renders its two dates in the **project's** zone; this endpoint has no converter call at all, so every timestamp comes back in UTC.

The three added state counts carry the same four exclusions as the counts they sit next to — both halves of the link live, the issue neither archived nor a draft — and a test counts the exclusions rather than trusting the eye.

The detail route `archived-cycles/<uuid>/` stays on Django, alongside its module twin: both carry the distributions and the burndown chart.

### The counts live on `cycleRow`, not on a row of their own

They are annotated by this list only, and the obvious shape — a struct embedding `cycleRow` embedding `Cycle` — is the one that does not work. GORM parses a struct embedded two levels deep as a **single** field and scans every column under it as zero **without erroring**. That cost a CI run earlier in this migration and there is a guard test naming it; this is the second time the same shape came up, so the comment sits on the fields.

## Migrated module: the archived module list

`GET` on `archived-modules/`.

It is the live list's twin and carries the same annotations, but the two querysets are not built the same way, and neither is their projection.

The live list goes through the viewset's base queryset, which narrows the modules to projects the caller is an active member of and drops archived projects. The archived endpoint is a plain `APIView` that builds from the manager directly, so it does neither: an archived project's archived modules still appear, and membership is left entirely to the permission class. Reproduced, not tidied.

The projection is twenty-six fields where the live one is twenty-eight. This one has no `logo_props` and neither estimate sum, and it carries `archived_at`. A test pins the difference from both sides, because it is the kind of thing that quietly converges when one list is edited.

Only `created_at` and `updated_at` are handed to the timezone converter. `archived_at` goes to the JSON encoder untouched, so it stays in UTC while its two neighbours move to the caller's zone.

The detail route `archived-modules/<uuid>/` stays on Django: it carries the estimate and issue distributions and the burndown chart, which the cycle app has not been through yet either.

## Migrated module: the module issue list and the two ways to link

`GET` and `POST` on `modules/<uuid>/issues/`, and `POST` on `issues/<uuid>/modules/`.

The list is the project list narrowed to one module, sharing its filtering, ordering, grouping and paging. Both halves of the link condition sit inside one EXISTS, the way the cycle list's do, because Django puts them in a single filter call and so applies them to the same joined row.

Adding a link **moves nothing**. An issue belongs to at most one cycle but to any number of modules, so where the cycle path deletes the old link first, this one only inserts, and a link that is already there is ignored rather than refused.

The two directions are not symmetric, and the asymmetry is upstream's. Adding issues to a module narrows the ids it is given to this project's live issues, which is what stops a foreign issue from being pulled in. Adding modules to an issue takes the module ids as given.

Removing through `issues/<uuid>/modules/` reads the module's name **before** the link goes, and sends `null` when the link was not there to begin with.

### The detail path stays on Django whole

`modules/<uuid>/issues/<uuid>/` is not cut over, and the proxy matcher stops at the collection. Django binds four methods there: `DELETE` is written by hand and works, but `GET`, `PUT` and `PATCH` fall through to the generic actions, and those serialize an **Issue** — which is what the viewset's queryset returns — with a serializer built for **ModuleIssue**. Its required `issue` field has no matching attribute on an Issue, so the read raises, and the writes set a stray attribute on the wrong model before failing on the way out. Cutting a path over means owning every method on it, so the Go handler for the delete exists but stays unregistered until those three are decided. This is the same call as `cycle-issues/<uuid>/`.

## Migrated module: module read, update, delete and archive

The four routes under `modules/<uuid>/`, and the archive toggle.

A module is archived on its **status** — completed or cancelled — where a cycle is archived on its **end date**. The two apps judge "finished" differently, and a test names the difference. And unlike a cycle, a module has no completed rule on update: a finished module can still be edited, and only an archived one is refused.

The member set is **replaced** rather than merged on update: the existing rows are soft deleted and the new ones inserted, so removing a member is a matter of leaving them out.

Deleting sends one activity per issue the module held, before the module goes, since each of those issues loses a module. Archiving clears **every member's** favourite, where deleting clears only the caller's — the same asymmetry the cycle routes have.

## Migrated module: the module list and create

`GET` and `POST` on `modules/`.

The list renders its timestamps in the **caller's** timezone, where the cycle list uses the **project's**. The two apps genuinely differ here and both are reproduced; a test names the asymmetry so neither drifts toward the other.

Every count and estimate sum reads through the `issue_objects` manager **and** requires a live link, so an issue removed from a module counts towards none of its totals. Only an estimate of the points type has a number to add, so a category estimate contributes nothing to either sum.

Creating checks the name against the project itself rather than letting the unique index answer, which is why a clash is a plain `400` with its own message.

The cutover guard earned its keep here: it refused the matcher when only `GET` was implemented, so the create landed in the same change rather than after a broken deploy.

## Migrated module: module favourites, saved views and links

The first slice of the module app, which mirrors the cycle one closely enough that the favourite writes are now shared: both entity types live in the one `user_favorites` table and differ only in the type they record.

Its favourite list is broken the same way the cycle one is — the viewset inherits DRF's `list` but declares no `serializer_class`, so `get_serializer_class` asserts and answers `500`. Reproduced, not invented.

The saved view is a `get_or_create` on read but not on update, and the update answers `201` even though it creates nothing. Both match the cycle route exactly.

The links are a full set of six methods, including the `PUT` that differs from `PATCH` only in requiring the url. The url goes through the same Django validator the issue links use.

## Migrated module: cycle date checks, favourites and saved views

The first slice of the cycle module: `cycles/date-check/`, `user-favorite-cycles/` and `cycles/<uuid>/user-properties/`.

`date-check/` answers whether a proposed interval overlaps a cycle that already exists. The interval is built by `convert_to_utc`, which is worth spelling out: a start date becomes the **first second** of that day in the **project's** timezone and an end date becomes **23:59** of it, which is what keeps two adjacent cycles from reading as overlapping. And a start date that falls on **today** in that timezone becomes the current instant rather than the start of the day, so a cycle created this afternoon does not claim to have begun this morning.

A clash is answered with `200` and a `status` of false rather than a `4xx`, since it is an answer rather than an error.

Favouriting writes unconditionally, so doing it twice hits the partial unique index and is a `400`. Unfavouriting removes the row outright rather than soft deleting it, which is what `delete(soft=False)` does.

The saved view is a `get_or_create` on read but **not** on update, so a `PATCH` against a cycle the caller has never opened is a `404`. The update answers `201` rather than `200`, even though it creates nothing.

## Migrated module: the issue sync list

`GET` on `v2/issues/`, which a client walks to keep a local copy of a project. It is the only issue list that pages with the **cursor** paginator rather than the offset one, and the only one ordered **ascending** — by `updated_at`, which is what lets a client resume from where it stopped.

Its three id arrays carry **no** soft-delete filter on the through table, which every other issue list does. So a soft-deleted label link still contributes its id here. That is reproduced rather than corrected, and a test asserts the two selects genuinely differ.

Its counts are raw rather than coalesced, so a null stays null. `description_html` is added only when `?description=true`, taking the projection from twenty-six fields to twenty-seven.

## Migrated module: the issue detail list

`GET` on `issues-detail/`, a flat paginated list that never groups. It differs from `issues/` in three ways.

It narrows a guest's view through a **permission subquery** rather than a separate check: a member above guest sees everything, a guest sees everything when the project opens all features to guests, and otherwise only what they raised. Django writes those three as one `Exists`, and so does this.

Its rows go through `IssueListDetailSerializer`, which is a plain twenty-three field dictionary rather than a `values()` projection. It carries no `state__group`, no `deleted_at` and no `description_html`, and its three counts are the **raw** annotations rather than coalesced ones — so a null stays null here where the paginated list turns it into zero.

`expand` may ask for `issue_relation` or `issue_related`, which are read in one query each rather than per issue. A relation whose far side is gone is skipped rather than rendered as null, and an expansion that was asked for is present as an empty list rather than absent, so a client can map over it either way.

## Migrated module: the bulk date update

`POST` on `issue-dates/`, which is what dragging a bar in the timeline view sends.

Each update is validated against whichever half of the pair it did not supply, so moving one end cannot cross the other. The dates are **parsed** rather than compared as text — Django runs them through `strptime`, which also means a malformed one raises and answers `500` rather than being refused with a `400`.

A falsy value is not a change. Django tests the parsed value for truth, so an explicit null or an empty string leaves the column alone rather than clearing it, and no activity is sent for it. An id that names nothing in this project is skipped rather than refused.

The write is a `bulk_update` of the two date columns, so no timestamp moves, and the previous value each activity carries is stringified — the literal `None` for a column that was null.

## Migrated module: the work item identifier lookup

`GET` on `work-items/<PROJ-42>/`, which resolves the human-facing key a pasted link carries. The key splits on the **first** dash, so a project identifier that contains one still resolves, and the sequence number goes through `strict_str_to_int` — digits with an optional leading minus and nothing else, so `1e3` or a padded number is a `400` rather than a `404`.

This is the only route that annotates `is_intake`, which is why every other one drops that key rather than returning it as false. Its response is therefore `IssueDetailSerializer` in full: twenty-eight fields.

Two smaller differences from the detail route, both taken from the queryset rather than assumed: the subscriber check reaches through the **sequence number** rather than the issue id, and the cycle annotation carries no soft-delete filter.

The project identifier is matched case-insensitively. The guest check runs **after** the issue is read, so a guest asking for an issue that is not there is told it does not exist rather than that they may not see it.

## Migrated module: the archived issue list

`GET` on `archived-issues/`, flat and grouped. It shares the live list's filtering, ordering, grouping and paging, and differs in three ways.

It reads through the **plain soft-delete manager** rather than `issue_objects`, narrowed to rows that are archived and are not epics — `issue_objects` hides archived rows by definition, so it could not be used here. It also does not exclude drafts, which `issue_objects` does.

It carries no guest narrowing and records no visit. And `show_sub_issues` defaults to true, so sub-issues are hidden only when a caller asks for that explicitly.

The manager the list reads through is now a parameter of the shared scope, which is what lets the two routes share everything else.

## Migrated module: the bulk issue read

`GET` on `issues/list/`, which reads a named set of issues rather than a page of them — what the client uses to refresh the issues it already knows about. It returns a bare list with no envelope, and refuses a request that names none.

Its projection is not the paginated list's. It carries `deleted_at`, which that one does not, and has no `state__group`, which that one does. It also adds one predicate the paginated list lacks: the issue's state must not be soft deleted.

The `fields` parameter has no effect on either endpoint. `DynamicBaseSerializer` pops it and then overwrites it with `expand`, so only `expand` changes the shape — and since `expand` only ever *adds* nested serializers, a request with `fields` alone gets the plain twenty-five field serializer. The expansion serializers are not ported, so a request that does name `expand` is an error rather than a body of the wrong shape; the only caller in the web client sends neither.

## Migrated module: creating an issue

`POST` on `issues/`. With this the paginated list is cut over again — it went back to Django in the cutover-guard change, because a matcher that serves only `GET` on a path Django also serves `POST` on is worse than no matcher at all.

Most of the work is in `Issue.save`'s adding path. The project is **locked** first, with an advisory key derived from the project id, because both the sequence number and the sort order are read from rows another request could be writing at the same moment. That key is `convert_uuid_to_integer` — the first eight bytes of the id's SHA-256 read as a **signed** big-endian integer — and the two implementations have to compute it identically: during the transition a create can arrive at either side, and a lock they disagree on is no lock at all. Its fixture is half negative on purpose, so reading the key unsigned fails the test.

The sequence number continues from the highest already handed out and starts at one. The sort order puts a new issue after everything already in its chosen state, and leaves it at the default when that state is empty. `workspace_id` comes off the **project** rather than the request, which is what `ProjectBaseModel.save` does.

An issue with no state of its own takes the project's default, and failing that whatever non-triage state comes first. If the chosen state is a completed one, `completed_at` is stamped at creation.

With no assignees of its own the issue goes to the project's **default assignee** — but only while they are still an active member at member level or above, which is the same floor the assignee field itself enforces. Both related sets ignore a conflict rather than failing the create.

## Migrated module: the issue list, ungrouped

`GET` on `issues/` without `group_by`, which is the flat path through the offset paginator. The grouped and sub-grouped paths still answer from Django, so **the proxy does not cut this route over yet**: the matcher works on paths, not query parameters, and moving `issues/` across would take the grouped requests with it. The handler is registered and tested; the Caddyfile line lands with the grouped paths.

The projection is `issue_on_results`: twenty-three fields plus the three id arrays, with the group key named `state__group` rather than `state_group` because that is the lookup path the projection asks for. `description_html` is not among them, so the list never carries issue bodies. The counts are left null by the query and become zero in the serializer, which is where an `IntegerField` does it.

A guest sees only the issues they raised, unless the project opens all features to guests. The set is counted before the annotations are applied and over distinct issues, since a filter's join can multiply rows.

## Shared: translating the issue filters into SQL

`internal/project/issue_filter_sql.go` turns the lookup dictionary `issueFilters` produces into the joins and conditions a query needs. Every expression was taken from the SQL Django renders for that lookup rather than written from the field name, which is what settles the parts that are easy to get wrong:

- A many-to-many value lookup reaches **only the through table**. Filtering by label id joins `issue_labels` and compares `label_id`; the `labels` table is never joined at all.
- A value lookup and its soft-delete companion share **one** join, so both conditions apply to the same row — which is the intended reading of "a label link that is not deleted and whose label is one of these".
- A **null** value lookup forces a `LEFT OUTER` join, because an inner one can never produce the row it is looking for. The soft-delete companion on its own does not widen anything.
- A `DateField` compares directly; a `DateTimeField` goes through `(column AT TIME ZONE 'UTC')::date` first. The deployment's `TIME_ZONE` is what that cast names.
- `icontains` is `UPPER(column::text) LIKE UPPER(?)`, with the caller's own `%`, `_` and backslashes escaped so they match themselves.

Two behaviours are reproduced rather than corrected:

- Asking for both the literal `None` and a specific label puts `label_id IS NULL` and `label_id IN (...)` on the same join, so the filter matches nothing.
- `logged_by` exists nowhere but in `issue_filters` itself — not on the model, not in any view — so Django cannot resolve it and raises `FieldError`, which answers `500`. The translation refuses rather than inventing a column, and a test asserts every **other** lookup the mapping can produce is translatable, so no other parameter can reach that path by accident.

## Shared: the offset paginator

There are three paginators in the Plane codebase and they share little but the word. `internal/pagination/offset.go` ports the second of them, `OffsetPaginator` with `BasePaginator.paginate`, which is what sits behind the issue list and the issue detail list.

It differs from the cursor paginator already ported here in every visible way. Its cursor's third part is a backwards flag rather than an inert offset, and its second part is a page number rather than a raw offset. Its envelope has twelve keys rather than nine, and reports the whole set twice — once as `total_count` and once as `total_results`. Both of its cursors are always strings, where the cursor paginator's `next_cursor` goes null on the last page.

It answers "is there a next page" by reading one row more than the page and seeing whether that row arrived, which is why the window reaches to `offset + limit + 1` and the response then takes only the first `limit` of them.

And `per_page` above the ceiling is **refused** with a 400 rather than clamped, which is the opposite of what the cursor paginator does with its page size. The ceiling itself is raised to the default when it would otherwise sit below it.

The arithmetic is diffed against the same expressions the class uses, over 200 combinations of page size, page number and total.

## Shared: the grouped paginator

`internal/pagination/grouped.go` ports the two halves of `GroupedOffsetPaginator` that are pure over their inputs: the offset arithmetic that decides which window of row numbers a cursor asks for, and `process_results`, which turns a flat page of rows into the grouped body the issue list returns. Both are diffed against the real class over a generated corpus, so neither rests on a reading of the Python.

The offset arithmetic is deliberately not the flat paginator's: this one multiplies by the page size baked into the cursor rather than by the request's limit, and falls back to the limit only when that size is zero.

The grouping is where the surprises are, and the corpus is what found them:

- Grouping by a plain column **pre-seeds every known group**, so a group with no rows on this page still comes back with an empty list and its total. Grouping by a many-to-many field does not — only the groups actually present appear.
- An empty page returns no buckets at all, even on the plain path.
- A many-to-many group-by makes the queryset fan one issue into several rows. The paginator collects the groups per issue, rewrites the issue's own id array from them, and files the issue under each. The row that lands in **every** bucket is the first one seen for that issue, since the duplicate check is by id alone — so in a later bucket the group-by field still reads as the first row's value.
- An issue in no group is filed under the literal string `None` and carries an **empty** id array rather than that string.

The id arrays Django builds come out of a Python set, whose iteration order is not stable across processes because string hashing is randomized. The order is therefore arbitrary rather than meaningful; the port sorts, and the generator sorts too, which is what makes the fixture reproducible — it was checked by regenerating it under several hash seeds.

## Shared: the issue filter mapping

`internal/project/issue_filters.go` ports `plane.utils.issue_filters`, which maps twenty-five query parameters onto ORM lookups. It is the other half of what the issue list needs, alongside the paginator.

The mapping is full of asymmetries that are invisible unless the two implementations are compared row by row, so it is diffed against the real function over roughly a thousand query strings — every filter against every interesting value, in both methods and both prefixes, plus a hundred and fifty random combinations, since the filters share one output dictionary and a later one can overwrite an earlier one's key. The relative date terms are measured from a frozen day so the fixture does not go stale overnight.

What the corpus pins:

- `GET` splits a value on commas; `POST` takes it whole and does no parsing at all.
- Some filters drop anything that is not a uuid; `state_group`, `estimate_point` and `priority` keep whatever they were given.
- A literal `None` becomes an `isnull` lookup on some filters and nothing on others.
- `labels`, `assignees`, `cycle`, `module` and `subscriber` each write a soft-delete lookup **unconditionally**, so asking about them at all narrows the join.

Two of the rows it pins are upstream bugs rather than design, and both are reproduced:

- The `POST` date branch hands `date_filter` the raw string rather than a list, so the loop iterates it one character at a time. `updated_at=2026-01-02;before` therefore writes an `__lte` of the empty string, from the lone semicolon, and a `__contains` of the last character.
- `filter_intake_status` guards on `intake_status` but stores the value of `inbox_status`, which is the Python `None` when that parameter is absent.

## Shared: the cursor paginator

`internal/pagination` ports `plane.utils.global_paginator`, which is the simpler of the two paginators in the codebase — the grouped one behind the issue list is still to come. Its cursor is a `"size:page:offset"` triple; only the first two are read, and the third is carried so the string keeps its shape.

The arithmetic is diffed against the same computation the Python performs, over 175 combinations of page size, page number and total. The edges are what the fixture is for: a page past the end, a page size above the thousand cap, a total that divides exactly.

Two behaviours are reproduced rather than tidied. `next_cursor` is **null** on the last page while `prev_cursor` is always a string, so on page zero it names page `-1`. And a cursor asking for a page size of zero reaches a division Django does not guard, so it answers `500` rather than an empty page.

## Migrated module: issue versions

`issues/<uuid>/versions/` and `work-items/<uuid>/description-versions/`, list and detail on both. These are the first routes on the Go side to page.

Both lists return the same ten-column projection, since a list is only ever used to pick a version to open, and only `created_at` and `updated_at` move into the caller's timezone — `last_saved_at` is not in the converter's list.

`IssueVersionDetailSerializer` names `name` twice in its field list. DRF silently dedupes, so the response carries thirty keys; `properties` and `activity` are on the model but not in the list, so neither is returned. The description version renders its `description_binary` as base64, and as null when the column is.

The description version routes carry an extra gate the issue version routes do not: a guest may only read the body history of an issue they raised, unless the project opens all features to guests. Django reads the issue with an unguarded `.get` before that check, so a missing issue is a `404` rather than a `403`.

## Migrated module: issue meta and project issue operations

Four small routes that needed no paginator: `issues/<uuid>/meta/`, `user-properties/`, `bulk-delete-issues/` and `deleted-issues/`.

`meta/` returns the sequence id and the project identifier, which is what the issue page needs to render the human-facing key before it has the issue itself.

`user-properties/` is the caller's own view preferences for a project. It is a `get_or_create`, so a first read writes the row rather than answering 404; the row is normally seeded when the member joins, so this only fires for a membership that predates the seeding. The serializer is `fields = "__all__"` with the three relations read-only — fifteen keys, a count taken from rendering it — and only the five json columns and the sort order are writable.

`bulk-delete-issues/` is admin-only. It soft deletes the cycle and module links first and then the issues, all three as queryset deletes, so no timestamp moves. The message counts the issues the filter matched rather than the ids the caller sent, and Django does not pluralise it, so one issue still reads "issues".

`deleted-issues/` reads through the unfiltered manager — the only way to see a soft-deleted or archived row — and returns a bare list of ids, newest first, so a client can drop them from its local store. `updated_at__gt` narrows it.

## Migrated module: issue activity

`GET` on `issues/<uuid>/history/`, for the two `activity_type` values the endpoint actually handles.

The history queryset is gated on the caller being an **active member of the project** and the project not being archived, both joined into the query rather than checked separately, so a non-member reads an empty list rather than a 403. Four fields are excluded — `comment`, `vote`, `reaction`, `draft` — because comments come back through their own serializer and the rest are not history. An activity with a null `field` survives that exclusion, which is what Django's `~Q(field__in=[...])` over a nullable column does.

The rows come back oldest first, unlike the model's own ordering, and `created_at__gt` narrows them.

`IssueActivitySerializer` is `fields = "__all__"` plus four nested details and `source_data` — twenty-five fields, a count taken from rendering the real serializer rather than from reading it. `source_data` is filled only on the `issue-property` branch, which is the only one that prefetches the intake row.

**The combined branch is broken upstream and reproduced as such.** When `activity_type` is anything other than `issue-property` or `issue-comment`, Django falls through to `sorted(chain(activities, comments), key=lambda instance: instance["created_at"])` — and a Django model instance is not subscriptable, so it raises `TypeError` and answers `500`. Three callers reach it: `issue.service.ts` sends no `activity_type` at all, and the epic variants the web client sends (`epic-property`, `epic-comment`) match neither branch. The Go side answers `500` there too rather than inventing a shape the frontend has never received.

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

## Shared: float rendering

`internal/drf.Float`, applied by `Respond` to every float it walks.

Go's `encoding/json` writes a float with the shortest digits that round-trip and a plain decimal point, so `65535.0` comes out as `65535` — indistinguishable from an integer. Python's `json.dumps` hands a float to `repr()`, which always leaves a decimal point behind, so the same value is `65535.0`. Every `sort_order`, every estimate sum and every progress total is a Django `FloatField`, which puts this on most responses rather than a few. It is the same shape of bug as the datetime rendering, and was found the same way: by reading what Python would actually print.

The two encoders also disagree about when to switch to exponential notation. `encoding/json` switches below `1e-6` and at or above `1e21`; Python switches when the decimal point would land past the sixteenth significant digit or more than four places left of the first one. Between those thresholds sit values like `1e16`, which Go writes out in full and Python writes as `1e+16`.

`FormatFloat` is CPython's `format_float_short` in `'r'` mode with `ADD_DOT_0`. The fixture is 519 rows keyed by the **bit pattern** rather than by a decimal string, so nothing is lost on the way into the test, and most of it is drawn uniformly from the 64-bit space rather than from round numbers — that is what reaches the awkward values. CI regenerates it against Python and diffs.

A value Python would write as `NaN` or `Infinity` is not JSON, and nothing in the schema can hold one, so `MarshalJSON` refuses rather than emit a body no parser accepts.

### A number out of a jsonb column is not one of these

`drf.DecodeJSON` reads blob columns with `UseNumber`, so a number inside `view_props` or `progress_snapshot` keeps the literal text it was stored with. psycopg hands Django the blob's own parse, so an integer stored there comes back an integer; Go's plain decoder turns every blob number into a `float64`, and after this change `Respond` would have rewritten all of them as floats. The three package-local `decodeJSON` helpers now call it.

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
