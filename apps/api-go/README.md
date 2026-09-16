# Pace Go API

This directory contains the incremental Gin, GORM, and PostgreSQL replacement for the Django API. Business modules are migrated in separate pull requests; Django remains the behavioral reference until a module passes its contract and integration tests.



## The space app is a third application

`internal/space` serves the published half of a project: what a person sees who has a link and no account. It is a third application beside the session API and the external API, with its own package, its own base view and its own idea of who is asking.

Nothing here carries a session. Every route is reached through an **anchor** — thirty-two hexadecimal characters in the url — and the anchor is the whole of the credential, so the first thing each route does is turn one into a deploy board and refuse the request when it cannot. There are two lookups: the settings and the metadata ask that the anchor be a **project's**, and everything else takes whatever board the anchor names, so an anchor published for a page or a view resolves the same way and its project columns are read off it regardless.

The two refusals are worded differently for no reason anyone recorded: the strict lookup answers the base view's `The requested resource does not exist.` and the loose one answers `Invalid anchor`, while the metadata answers `Project is not published`.

## The external API is a second application

`internal/externalapi` serves `plane.api`, the key-authenticated surface integrations call. It is not the session API with a different prefix — it differs in five ways, and mixing them up is how an integration breaks:

- **Authentication** is an `X-Api-Key` header against the `api_tokens` table, not a session cookie.
- **Rate limiting** is per key, with the key's own limit taking precedence over the configured default.
- **Errors** have their own vocabulary. A missing object is `{"error": "The requested resource does not exist."}`, not `{"detail": ...}`.
- **Serializers** are a different set. Its `UserLiteSerializer` carries the **email**, which the session API's reveals only to an admin.
- **Create answers 200**, not 201.

### It found a live regression

`/api/v1/users/me/` had already been cut over — to the **session-authenticated** user package. Every integration calling it with a key and no cookie was getting a `401` where Django answered. It is now served by this package with the right authentication, and a test refuses any `/api/v1/` route registered in the user package.

### The rate limit is per process here

Django keeps the throttle window in its cache, which is Redis in a deployment, so every process shares one budget. This keeps it per process while both halves run side by side — sharing the counter would mean the two APIs throttling each other. The headers are set only on an **allowed** request, which is upstream's doing: a refused one carries no remaining count.

DRF reads only the **first letter** of the period, so `60/month` is sixty a minute. Ported as written, with a test.

### Two ways to be unauthenticated

A request with no key at all is not refused by the authenticator — it returns nothing and lets the permission class answer. A key that is present and wrong gets the authenticator's message. Two bodies, both `401`.

Every authenticated call writes the key's `last_used`, so every request is a write even when the route only reads.

## Migrated external module: the module list, create, detail, delete and work items

`GET` and `POST` on `modules/`, `GET`, `PATCH` and `DELETE` on `modules/<uuid>/`, and the four routes under `module-issues/`, are implemented and cut over.

Two fixes to the module shape, which the archived list merged earlier also renders. `members` is on **every** module body: the declared field is write only, but the serializer's own `to_representation` puts the ids back, which the port had dropped. And the membership list carries no soft-delete filter, because Django reads it as a many-to-many rather than through the link's own manager — a removed membership still names its member. The two planning dates are `DateField`s and render as the day alone; the archive stamp is a `DateTimeField` here, unlike the work item's, so it keeps its time.

The list and the detail read an annotated queryset and answer with twenty-nine fields. The create and the update read the module back through a plain one, so the six counts are absent from those bodies — twenty-three fields.

A name that is taken is refused twice over, in two shapes: the create answers four flat keys (`id`, `code`, `error`, `message`) and the update answers one, both flat rather than the lists a field error carries, because the serializer raises them from `create()` and `update()` where nothing wraps them. The member list is **narrowed** rather than refused — an id that is not a member of the project is dropped and nobody is told — and the narrowing asks only for a membership row, not an active one, so somebody removed from the project can still be put on a module.

The work item routes under a module differ from the cycle's in three ways. The **detail** answers with a page holding one work item rather than with the link. The create only ever **adds**: the loop meant to move a work item out of another module compares a string against a queryset of UUIDs, which is never equal, so the branch that would move one is dead — a work item already in another module ends up in both, and the unique index is what keeps one already in *this* module from being added twice. And the ids it acts on come from the plain manager, so an archived, draft or triage work item can be put into a module even though the module's own list will not show it afterwards.

The activity that create queues carries `requested_data` as the **repr of a queryset** rather than as a list — `<QuerySet [UUID('…')]>`, truncated after twenty entries the way Python truncates one — because Django builds it with `str()` over a queryset. Nothing parses it, and reproducing it is cheaper than explaining a difference in a log.

## Migrated external module: the module picker, archive and archived list

Six routes, the same shape as the cycle ones and with the same doubled archive pair — one class at two paths, dispatching on the method name, so both handlers answer on both.

A module is finished by its **status** where a cycle is finished by its **end date**. The same asymmetry the session API has, one app over, and reproduced in both.

Every count in the archived list is over **distinct** issues and requires a live link, so an issue linked twice counts once and a removed one not at all.

### The members are annotated and never rendered

The `members` field is **write only** on the serializer, so neither the lite nor the full shape reports who is on a module — the archived list computes the ids and then drops them. Kept, because computing them is what the queryset does.

The archived module list does **not** check membership at all: its queryset filters the workspace and the project and nothing else, leaving the permission class to do the whole job. The archived **cycle** list beside it does join the membership. Two neighbours, two rules.

The lite list accepts an order parameter and ignores it, for the same reason its cycle twin does.

## Migrated external module: the cycle transfer

`POST` on `cycles/<uuid>/transfer-issues/` is implemented and cut over, which completes the external cycle module.

The work itself is the utility the session API's own transfer runs, and it stays in `internal/project` rather than moving to a package of its own: the snapshot it freezes **is** that API's analytics — the two distributions and the two burndowns those endpoints compute — so moving the transfer would mean moving them. The external route calls in and publishes its own activity afterwards, since the two APIs queue it with different keywords.

What this route adds is a guard of its own: the **old** cycle has to be finished, which the session API never asks. A cycle with no end date at all passes, since there is no date to be past.

## Migrated external module: the work items in a cycle

`GET` and `POST` on `cycles/<uuid>/cycle-issues/`, and `GET` and `DELETE` on `<issue>/` under it, are implemented and cut over. The transfer is the one route under a cycle still on Django.

The two halves of this module answer with different serializers. The **list** returns the work items themselves, through the same twenty-nine field serializer the work item list uses, while the **detail** returns the link — eleven fields with the work item's child count beside them. The create answers with every link in the cycle rather than the ones it just wrote, and with a `200` rather than a `201`.

The list orders with a bare `order_by` rather than through the `Case` machinery the project's own list uses, and `testdata/plain_issue_order_by.tsv` pins what that means. `priority` sorts the **words** — high, low, medium, none, urgent — rather than the severities, because nothing maps them onto an order. The three fields that reach through a multi-valued relation add joins that **repeat** a work item once per related row, since the queryset carries no `distinct()`: a work item with three labels is three rows of a list ordered by label name. The fixture records the join count for exactly that reason, and the fallback here is ascending by creation rather than the descending one the project list falls back to.

A work item already in another cycle is **moved** rather than copied, and the move writes only the cycle column, so no timestamp on the link changes. A work item the manager cannot see — archived, draft or triage — is dropped from the request rather than refusing it. `bulk_create` goes around `save()`, so a new link records nobody as its author.

One deliberate divergence: the links being moved are scoped to the caller's workspace and project. Django's query is not, which would let a caller pull another workspace's links into their own cycle by naming its work item ids. That is the same hole that was closed on the session API (GHSA-4w5x-wc9w-f47x), so it is closed here rather than carried over.

The activity the create queues carries `created_cycle_issues` as a JSON **string** inside the snapshot rather than as a nested object, because Django builds it with `serializers.serialize` and then dumps the whole snapshot around it; the task calls `json.loads` on it, so the nesting has to survive.

## Migrated external module: the cycle list, create, detail and delete

`GET` and `POST` on `cycles/`, and `GET`, `PATCH` and `DELETE` on `cycles/<uuid>/`, are implemented and cut over.

`cycle_view` narrows the list, and `current` is the one value that answers a **plain list** rather than a page — every other value, including one nobody recognises, comes back in the paginated envelope. The list queryset annotates the six counts and not the three estimates, so the estimate keys are **absent** there while the archived list carries them: a read-only field with nothing behind it is dropped by DRF rather than rendered as null. The create and the update answer over a plain instance, so none of the nine numbers is on those bodies at all — twenty-eight fields on a list row, twenty-two on a create.

The two dates go together, both or neither, and the pair is refused before the serializer is built — which is why that message is worded from the outside rather than as a field error. Only the **day** of each survives: the pair is rewritten as the project's day boundaries, a start becoming the first second of the day and an end its last minute, so two cycles that meet do not read as overlapping. A start that falls on today becomes the current instant instead, so a cycle created this afternoon does not claim to have begun this morning. That conversion now lives in `internal/cycles`, because the session API's date check runs a cycle's dates through the same one.

A new cycle is placed **ahead** of the ones already there — the smallest sort order less ten thousand — which is the opposite of how a new work item is placed.

Two behaviours are reproduced rather than tidied:

- A cycle whose end date has passed is meant to be frozen but for its sort order. The narrowing is written and then dropped on the floor: the serializer is handed the original payload rather than the narrowed one, so a completed cycle really can be edited in full as long as the payload names a sort order at all.
- The delete queues its activity **before** the cycle is gone, so that it can still name the work items that were in it, and removes the favourites for good rather than soft deleting them. Like the work item delete it names no notification and no origin.

Deleting is narrower than the permission class the route carries: only an admin or the person who **owns** the cycle may, and it answers `Only admin or creator can delete the cycle`.

## Migrated external module: the cycle picker, archive and archived list

Six routes: `cycles-lite/`, `archived-cycles/`, and the archive pair — which is four routes rather than two.

### One class, two paths, both methods on each

`CycleArchiveUnarchiveAPIEndpoint` is mounted at `cycles/<id>/archive/` **and** at `archived-cycles/<id>/unarchive/`, and an `APIView` dispatches on the **method name** rather than on the path. So both handlers are reachable on both paths: archiving through the unarchive path is nonsense and it is what Django serves. All four are registered.

A cycle is finished by its **end date** rather than by any status — and one ending at this very moment has not ended yet, since the comparison is strict. A cycle with no end date is never archivable. Archiving clears **every** member's favourite of it; unarchiving does not put them back.

### The estimates sum the key, not the value

The archived list's three estimate sums add up `estimate_point__key` — the **position on the scale**, not the points it stands for. A scale of 1/2/3/5/8 reports 1/2/3/4/5, so these numbers do not match the session API's estimate sums, which add the value. Reproduced, with a test naming the difference.

### The lite list accepts an order and ignores it

Its `sanitize_order_by` call omits the allowlist argument, so the **empty** default is used and every field falls back to `-created_at`. The parameter is accepted and then has no effect, whatever it names.

The lite serializer carries **no counts at all**; the full one carries nine.

## Migrated external module: user assets

`POST` on `assets/user-assets/`, `PATCH` and `DELETE` on `<uuid>/`, and the same three under `server/`, are implemented and cut over.

A profile image belongs to a **person** rather than to a workspace, so nothing here is scoped to one and the asset key has no workspace in front of it — the only asset key in this API that does not. The type list is narrower than a work item attachment's: five image types and nothing else. A name that sanitizes away to nothing is stored as `unnamed` rather than refused, and the entity has to be `USER_AVATAR` or `USER_COVER`.

The `PATCH` rewrites the attributes from the payload, which the workspace asset route does not. The `DELETE` takes the image off the person as well as marking it deleted, and it marks it with **both** `is_deleted` and `deleted_at` — so a query that filters only on `deleted_at` still sees it.

`POST assets/user-assets/server/` has never worked. It asks the storage class for server credentials by passing an argument that class does not take, which is a `TypeError` and therefore a `500`. The Go port answers the same `500` rather than quietly fixing it: a route that starts working is a change no caller asked for, and the error it raises is the only behaviour it has ever had. The `PATCH` and `DELETE` under `server/` do work, because neither touches the storage, and they are the same two handlers as the plain pair.

## Migrated external module: generic assets

The three routes under `/api/v1/workspaces/<slug>/assets/`: reserve, download, and mark uploaded.

### A script-capable type is served as an attachment

Seven types — SVG, HTML, XHTML, XML and the two JavaScript spellings — are served with an `attachment` disposition rather than inline. That is what stops an uploaded SVG or HTML file running as script on the workspace's own origin.

The stored type is **cut at the first semicolon and lowercased** before the list is consulted, so `Text/HTML; charset=utf-8` is caught. Worth a test, because a normalization that was skipped would be a hole rather than a bug.

### The size defaults to the cap

An integration that omits it reserves the **largest allowed upload**, not nothing. And the guard tests the size for **truthiness**, so a size of zero is refused along with a missing name. It is read with `int()`, so a string of digits works and anything else raises.

The row exists before the upload does, and an integration that never finishes leaves a row behind that no route cleans up. The conflict body says `message` where every other error in this app says `error`.

The metadata task is queued **before** the flag is written and regardless of whether the request actually turned it on — so a repeated call with no body queues it again for an asset that already has metadata. Only the flag is written: the save names one field.

### Shared: what a file may be called and what it may be

`internal/uploads` now holds `SanitizeFilename` and `AttachmentMimeTypes`, which both APIs need and which were living in the session one. Both are settings rather than code, and the filename rule is checked against Python over a corpus — so one copy, one fixture. The session API's attachment routes use it unchanged.

## Migrated external module: intake

The five routes under `/api/v1/.../intake-issues/`.

The list **hides what is still snoozed** — a work item put off until tomorrow is not in the list today — which is a narrowing the session API's version does not have.

### The list and the writes read the same two facts differently

Whether the project has an intake row, and whether the feature is switched on. The **list** empties when **either** is missing; the **writes** refuse only when **both** are. So a project with the feature on and no intake row has an empty list and a create that is let through — and then raises on the intake it did not find. Reproduced, with a test walking all four combinations.

### The create skips almost everything the session API does

The work item is written directly rather than through the serializer, so it gets **no sequence number, no default assignee and no sort order of its own**. And the description is sanitized with the validity flag **discarded**: a rejected one becomes the empty paragraph rather than an error.

### Two halves, gated apart again

A **guest** may edit the work item and only its name and description; anything else is dropped. The **queue entry** moves only for a role **above** member, so a plain member may edit a work item and not triage it. Somebody who is not in the project at all raises rather than being refused, because the role is read off an unguarded `.get`.

Deleting takes the work item with it unless it was **accepted**, and removing the work item wants the person who raised it or a project admin — the queue entry goes either way.

The intake is reported **twice** in the body, once under its own name and once under the one it had before intake was called inbox.

## Migrated external module: stickies and invitations

Ten routes, and a correction to the route inventory that made them checkable.

### The route table was not seeing DRF routers

Both of these are mounted through a `DefaultRouter` rather than `path()` entries, so the generator was writing their raw regular expressions into the inventory — `^stickies/(?P<pk>[^/.]+)/$` and the like. Those strings match nothing, so the cutover guard was **silently not checking** either path.

The generator now normalizes a regex pattern into the same `<name>` notation, which surfaced two things the inventory had been hiding: the router binds **PUT** on both detail paths, and it appends a **format-suffix** route to every one of them. The suffix routes are kept in the table rather than dropped — they are paths Django really serves, and the guard is only useful if the table is complete. Nothing here claims them, so they stay on Django.

### A sticky is private to its owner

The queryset narrows to the **owner**, not to the workspace's members, so no role lets one person read another's. The name is not required, which is unusual — a note can be saved with nothing but a colour. The search looks at the **stripped** text rather than the html, so a note is found by what it says and not by how it is marked up. And this list's page is **twenty** where every other list in this API defaults to a hundred.

Its `PUT` is exactly its `PATCH`, because the serializer requires nothing at all.

### An invitation's two update methods disagree about the same field

`PATCH` **refuses any request naming an email**, with a code of its own. `PUT` **requires** one — a full update is not partial — and the serializer then refuses any address already invited in this workspace, which includes the invitation's **own**.

So the only `PUT` that succeeds is one naming an address nobody has been invited with, and it does the very thing the `PATCH` exists to forbid: it changes the address. Both reproduced.

Withdrawing an invitation refuses one already answered, and the two refusals are **ordered** — accepted is checked before responded, so an invitation that is both reports the first. The token is never rendered: it is what accepting the invitation proves.

The list is one of the few in this API that is **not paginated**.

## Migrated external module: labels

The five routes under `/api/v1/.../labels/`.

The same pair of conflicts the external states have — a repeated **external id** checked before the write, a repeated **name** caught from the database afterwards — but the external-id check is **not the same rule**. The label's needs **both** halves in the request; the state's fires on the id alone, comparing it against the value the state already holds. So an integration changing only the id gets through on a label and is refused on a state. Both reproduced, with a test stating the difference.

On update the conflict body carries the id of the label **being edited**, not the one that clashed — the opposite of what the message suggests, and what makes it useless for finding the duplicate.

The list narrows by the `fields` parameter and the retrieve does not, because the retrieve never reads it.

### The external estimate routes are dead and stay that way

`plane/api/urls/estimate.py` exists and defines three endpoints. It is **never included** in the external API's URLconf, so Django answers `404` for all of them — verified with the real resolver. They are deliberately not migrated: cutting them over would invent routes that do not exist upstream, which is a behaviour change rather than a port. A negative case in the proxy test records it.

## Migrated external module: members

Thirteen routes. The project member endpoints are mounted under **both** `members/` and `project-members/`, with every method bound on each — serving one of the pair would leave half the integrations on Django.

### Two siblings disagree about a missing workspace

`members/` calls it a **400** and `members-lite/` calls it a **404**, with the same message in both. Reproduced rather than reconciled. The lite project list also checks that the **project** exists, which its full sibling does not.

### The list reads the users, not the memberships

So it carries **no role and no active flag**, and it does not filter the inactive out either — somebody removed from a project is still listed by it. The lite list flattens both together and does carry them.

The retrieve answers with the **person** too, so the role it was looked up by does not appear in the body.

### Adding and removing

Adding grants rather than invites: the person has to be in the **workspace** already. The role is checked against the three the model names, so a number outside them is refused rather than stored.

Removing switches the membership **off** rather than deleting it — `is_active`, not `deleted_at` — which is the difference from every other destroy in this codebase, and what keeps a departed member's history attributable.

The three write routes swap `ProjectAdminPermission` in for the read one's `ProjectMemberPermission`.

## Migrated external module: the work item list and detail

`GET` on `issues/` and `work-items/`, and on `<uuid>/` under either name, are implemented. Both spellings carry both routes here, unlike the attachments where each name has a path of its own. The write methods are still Django's, so neither path is cut over yet.

`internal/externalapi/issue_order.go` is the ordering branch of the list, which the view writes inline rather than sharing with `order_issue_queryset` — and the two do not agree. `testdata/issue_order_by.tsv` is the `ORDER BY` Django renders for each of the fourteen allowlisted fields in both directions, extracted from Django rather than written by hand, and it pins three differences that reading the two implementations side by side does not show: this ordering carries **no secondary key**, it **reverses the priority list** for a descending sort rather than ordering the same way twice, and its state `Case` carries a **default** so a group outside the five sorts last rather than as a null.

One allowlisted field cannot be served at all. `order_by=issue_module__module__name` reaches the module name through a join and leaves the column out of the select list, which a `SELECT DISTINCT` refuses — so the request fails in the database. The Go port fails it too, with the same body, rather than quietly returning work items in some other order.

The view annotates a cycle id, a link count, an attachment count and a sub-issue count onto every row and then renders them through a serializer that declares none of them, so all four are computed and thrown away. They are not read here.

`external_id` and `external_source` together turn the list into a single-work-item lookup that is not paginated and reads through the **plain** manager rather than the issue manager, so it is the one way to reach an archived, draft or triage work item through this route. Two work items carrying the same pair answer `500` rather than picking one. `pql` and `filters` are refused by name with a `400` naming them, since community builds have no query language.

`expand` replaces an id with the record it names: `state`, `project`, `workspace`, `created_by`, `updated_by`, `parent`, `estimate_point`, `assignees` and `labels` each have a serializer of their own, and each is read in one query for the whole page rather than per work item. Two details come from DRF rather than from the view. A relation that is **null** expands to an empty object rather than to null, because a serializer handed `None` renders the empty mapping. And a name the expansion table does not know falls back to the instance's own `<name>_id`, which for everything but `type` does not exist and lands as null; a name the serializer does not declare at all is ignored.

Three renderings are corrected here for the whole external work item shape, which the reference lookup migrated earlier shares. `start_date`, `target_date` and `archived_at` are `DateField`s and render as the day alone rather than as a midnight instant, and `point` is an `IntegerField` and must not be read as a float — every float this API renders carries a decimal point, so a point read as one would come back as `3.0`.

## Migrated external module: the work item create, update and delete

`POST` on `issues/` and `work-items/`, and `PATCH` and `DELETE` on `<uuid>/` under either name, are implemented, and both paths are now cut over. Django binds no `PUT` on either of them: the serializer has a `put` method for upserting by external id, but the URLconf never routes to it, so it is dead and is not ported.

`testdata/issue_validation.tsv` is the body DRF answers with for sixty payloads, generated rather than written by hand and diffed in CI. An integration reads these messages, so a message that differs is one it cannot match. It is what pins the details: every field is checked before any of them is rejected, so two bad fields answer with both, and `validate()` runs only when none of them failed — which is why a bad date never reports the date comparison as well. It also pins the coercions, which are not obvious from the field types: `name: 5` is the string `"5"` while `name: true` is `Not a valid string.`, `point: "12"` is twelve while `point: 1.5` is not an integer, and `deleted_at` takes a day on its own where `start_date` refuses a datetime.

`description_html` is the field that changes on its way in. It goes through libxml2 and then the sanitizer, so `<p>a</p><p>b</p>` is stored wrapped in a `div` — see the lxml round-trip above — and `description_html: ""` is **refused** with `Invalid HTML passed` rather than stored, because markup that parses to nothing is a parser error.

Three behaviours are reproduced rather than tidied:

- The create answers with the work item as it stood **before** its audit columns were rewritten. Django reads `serializer.data` once to find the id it just wrote, which fixes the body then and there, and the `created_at` and `created_by` the caller asked for are written after that. So an import that carries its own dates is answered with today's, even though the row holds the dates it asked for.
- The delete names no notification and no origin on the activity it queues, unlike the create and the update, so the task falls back to its own defaults.
- `webhook_event = "issue"` is declared on all three endpoints and read by nothing; the webhook goes out through `model_activity`.

Where this API and the session API part company: an assignee who is not an active member at member level or above is a **refusal** here, naming the identifiers it turned down, where the session API drops them silently. The same goes for a label outside the project. Deleting is narrower than the permission class the route carries — only an admin or the person who raised the work item may, whatever the route allows — and it answers `Only admin or creator can delete the work item`.

The update reads through the plain manager rather than the issue manager the list uses, so an archived, draft or triage work item can be edited and deleted here even though the list will not show it.

## Migrated external module: work item attachments

`GET`, `POST`, `PATCH` and `DELETE` on `issues/<uuid>/issue-attachments/` and on `work-items/<uuid>/attachments/` are implemented. The two spellings are two different paths here rather than the same word in two places: the older one reads `issue-attachments` under `issues`, the newer one reads `attachments` under `work-items`, and neither serves the other's shape — so the proxy matcher names both explicitly rather than accepting a wildcard between them.

These routes do not use the project permission class the rest of the external API uses. They call `user_has_issue_permission`, which passes the person who **raised** the work item whatever their role, and otherwise asks for an active membership at admin, member or guest level. That is what lets a guest manage the attachments on an item they filed themselves. The download check calls it with no roles at all, and with no roles the role filter is skipped entirely, so any active project member may fetch the bytes regardless of level.

Three details are reproduced rather than tidied, all of them divergences from the generic asset route that does the same job one level up:

- The create answers `200`, not the `201` its sibling answers, and refuses a missing name or size with `{"error": "Invalid request.", "status": false}` where the workspace asset route words the same guard as `Name and size are required fields.`. It also has no default size, so an absent `size` is nothing and nothing is refused.
- The external id conflict says `Issue with the same external id and external source already exists` although it is the attachment that clashed, not the work item.
- The download always serves `disposition="attachment"` and answers an HTTP **redirect** rather than a JSON body — the only route in this API that does. The generic asset route decides the disposition per type; this one never consults the type at all, so nothing uploaded here is ever rendered inline.

The detail lookup is scoped to the workspace and the project but **not** to the work item in the URL, so an asset id belonging to a sibling item in the same project resolves. That is upstream's query and is left as it is. The delete queues the storage-metadata task on its way out for an attachment that is going away, which is harmless rather than useful, and is likewise left alone.

## Migrated external module: the work item search and reference lookup

Four routes: a search and a lookup, each under both names.

### The one route addressed by something other than a uuid

`work-items/PROJ-42/` names a work item the way a person would. Django captures **twice in one path segment** — the project's identifier and the number, with a hyphen between — and a Gin route cannot: it captures the whole segment and splits the text itself. The split is at the **last** hyphen, because a project identifier may not contain one.

That difference broke the cutover guard, which compares path shapes: Django's was `*-*` and the Go one `*`. The guard now collapses any segment holding a parameter down to a lone star before comparing, on both sides — both shapes describe the same URL, and without it the guard reports a served route as unserved. Worth fixing properly rather than special-casing, since the next multi-capture route would hit it too.

A number that is not a number answers `500` rather than `404`: Django matches the segment as two strings and then compares the second against an integer column.

### The search has no length guard

Unlike its session-API cousin, which skips the sequence branch for a query over twenty characters, this one scans **every** query for numbers.

An **empty** search answers an empty list — the opposite of the session API's workspace search, which treats an empty query as no filter at all and returns everything. Two searches, two readings of the same emptiness.

The limit is parsed with `int()`, so a value that is not a number raises. And the endpoint has **no permission class at all**: the base view asks only that the caller is authenticated, and what keeps it honest is the queryset, which narrows to projects the caller is an active member of.

The issue serializer renders the two many-to-many sets as **lists of ids**, and reports the type twice — once as the relation and once as its id.

## Migrated external module: work item relations

Two routes — and the **one** work item route in this API mounted under a single name. The urls list `relations/` for `work-items/` and not for `issues/`, so the older spelling answers `404` and nothing here claims it.

### Only three kinds are ever stored

`blocked_by`, `start_before` and `finish_before`. Their opposites — blocking, start_after, finish_after — are the same rows read **from the other end**, which is why creating one of those writes the relation backwards. So one stored row fills two buckets in the list depending on which end the work item sits at, and the response renders through a different serializer for each direction: the forward one names the related work item, the reverse one names the work item it was written from. The id in the body is the other party either way.

### Two kinds are deduplicated and four are not

A duplicate or relates_to relation is symmetric, so the same pair could be reported twice and a seen-set holds it back. The four directional kinds have no such set: a pair recorded twice is reported twice.

The set is keyed on the **other** work item alone and shared across both directions, so a work item related to the same other one under both a duplicate and a relates_to reports only the first.

### An implemented_by relation is stored and never reported

The mapper names it, the create stores it, and the grouping has no branch for it — so it goes in and never comes out of this endpoint. Kept, with a test saying so.

Both the list and the create are **workspace-wide** rather than project-scoped, so a relation may cross projects within one workspace. A pair that already exists is skipped silently rather than refused.

## Migrated external module: work item activities

Four read-only routes, mounted under both `issues/` and `work-items/`.

A history **reads forwards**: the default order here is ascending, where every other list in this API is newest first. Two fields are orderable and nothing else reaches the query.

Four kinds of entry are never shown — a comment's own activity, because the comment routes report it; votes and reactions, which belong to the space app; and a draft's, which describes work that was never published. The exclusion is a `NOT IN`, and a null is not in any list, so an entry with **no field at all** survives it. The query says so explicitly rather than relying on that.

The detail route answers a **different 404** from every other route in this app: `{"message": ..., "code": "NOT_FOUND"}` rather than the base view's `{"error": ...}`.

## Migrated external module: work item comments

Ten routes, mounted under both `issues/` and `work-items/`.

### A comment can be imported with somebody else's name and yesterday's date

An integration may name both the **author** and the **creation time**, which is what lets it import a conversation with its original timestamps. Neither is checked.

The two activities that follow are attributed differently: the **model** activity to the caller, the **issue** activity to the named author. So an imported comment is announced by one and recorded by the other.

### The serializer excludes rather than lists

It names `exclude` instead of `fields`, so the stripped text and the document tree are the **only** things held back — everything else the model grows arrives in the body automatically. Nineteen fields today.

The external-id check on update compares against the comment's **own** id, so rewriting a comment with the id it already has is not a conflict; and the source it compares against falls back to the comment's when the request does not name one.

A rejected description is answered with the sanitizer's own wording under `comment_html`, where the sticky route answers with a message of its own under `error`. Two routes, one sanitizer, two shapes.

A comment defaults to **internal**, so one an integration creates is not public unless it says so.

## Migrated external module: work item links

Ten routes — five under `issues/` and the same five under `work-items/`, which is how **every** work item route in this API is mounted: under the current name and the one it had before an issue was called a work item.

### The create validates and the update does not

The create goes through `IssueLinkCreateSerializer`, which checks the url's **shape** and then its **scheme** — Django's validator accepts `ftp://`, and the scheme check is what narrows it to the two — and then refuses a url the work item already carries.

The update goes through the **full** serializer instead. It has neither check: any string at all may be written over a url, and a duplicate is accepted. An `IssueLinkUpdateSerializer` with both checks exists in the same file and **is not used by the update route**.

The duplicate check that does run is scoped to the **issue** rather than to the project, so the same url may hang off two different work items.

### The creator may be named in the body

The row is written and then its author is rewritten from `created_by` — which is how an integration attributes a link to the person it acted for rather than to the key. Nothing checks that the named person exists or is in the workspace, and the activity that follows is attributed to them rather than to the caller.

The crawler is asked for the page's title on create, and on update **only when the url actually changed**.

## Migrated external module: the project list and create

`GET` and `POST` on `projects/` are implemented, and the collection is cut over beside the detail.

**A fix to the project shape**, which the detail merged earlier also renders: the serializer reports **forty-four** fields and the port rendered twenty-one. Everything a project carries beyond its name — the five feature switches, the two automation windows, the description, the logo, the timezone, the external pair, the four relations and the two audit users — was missing from the retrieve and the update as well as from the list.

Creating a project is more than a row: the six workflow states come from `DEFAULT_STATES`, the caller is made an administrator, and a named lead who is not the caller is made one too. Every membership seeds a per-person ordering so the new project sits at the top of that person's list. All of it happens in the model rather than in the view, so it now lives in `internal/projects` where both APIs reach it.

Four details are reproduced rather than tidied:

- The identifier is upper-cased and trimmed before anything is written, which is the model's doing rather than the serializer's.
- A project with no timezone of its own takes the **workspace's**, read on the way in.
- With no `logo_props` the serializer picks an icon and a colour **at random** from two fixed lists, so two projects made from the same payload do not look alike.
- The identifier table is read and never written by this endpoint — only the session API writes it — so the identifier check only ever sees what the other API put there. What actually stops a clash is the database, and both of its unique constraints answer with the same message: `The project name is already taken`, even when it was the identifier that clashed, because Django reads the database's own wording and cannot tell them apart.

The lead has to be an **active** member of the workspace and its refusal is reported under its own key; the default assignee only has to be a member at all and its refusal is a non-field one. Two checks that read alike and are not alike.

## Migrated external module: the project detail

`GET`, `PATCH` and `DELETE` on `/api/v1/workspaces/<slug>/projects/<uuid>/`.

### A project cannot be called "Q1 (planning)"

`FORBIDDEN_IDENTIFIER_CHARS_PATTERN` is applied to the **name** as well as the identifier, and it refuses brackets, ampersands, hyphens, dots, percent signs and apostrophes among others. So `Auth & billing`, `front-end` and `v1.0` are all refused as project **names** through this API. Surprising, reproduced, and tested with seventeen cases.

An **archived** project refuses every change, checked before anything else runs.

### intake_view is written whether the request names it or not

It is read back out of the request with the project's own value as the default and then written unconditionally. And turning it on creates the project's default intake when it has none — with a name built from the project's name **as it was before this request**, so a request that renames the project and enables intake in one go names the intake after the old name.

### Deleting clears one favourite, archiving clears them all

The delete's favourite clear names the project **twice** — as the entity and as the scope — so it only removes somebody's favourite of the project itself. The archive route beside it clears the favourites of everything inside the project. Two routes on the same object, two scopes.

The permission is `ProjectBasePermission`, which is three rules in one: a safe method wants any active workspace member, a create wants an admin or member of the **workspace**, and everything else wants a **project** admin — or a workspace admin who is also in the project.

The project list and create stay on Django: they need the default-state seeding, which is the session API's to share first.

## Migrated external module: the project picker, archive and summary

`GET` on `projects-lite/`, `POST` and `DELETE` on `projects/<uuid>/archive/`, and `GET` on `projects/<uuid>/summary/`.

The picker offers the projects the caller is an active member of **plus every public one** — the one place in this API where membership is not required to see something. Its serializer is nine fields and nothing else: no network, no lead, no counts.

The `include_archived` switch reads `true` or `1` after lowercasing, and nothing else. Asking it to order by `sort_order` **fails**: the lite list never joins the membership that carries one, so Postgres refuses the column rather than falling back. Reproduced rather than papered over.

### Archiving takes the favourites of everything inside with it

The clear is by **project** rather than by entity, so a favourited cycle or page in an archived project stops being favourited too. Unarchiving does **not** put them back.

Both carry `WorkSpaceAdminPermission` rather than the project permission the rest of this app uses.

### The summary counts almost nothing through a manager

Seven of the eight count rows of their own table, **soft-deleted ones included** — the subqueries are built from the plain default managers, and those only filter the model they are asked about. The eighth, the issue count, excludes **triage alone**: not archived issues, not drafts.

Asking for nothing valid asks for all eight.

## Migrated external module: states

The five routes under `/api/v1/.../states/`.

The **triage** state is hidden from the list and the retrieve entirely — it is an implementation detail of intake. But the update's queryset does **not** exclude it, so an integration that knows the id can edit the state the list never showed it.

Creating has two different conflicts: a repeated **external id**, checked before the write, and a repeated **name**, caught from the database afterwards. Both carry the id of the state that was already there, which is what makes them useful to an integration replaying a sync. On update the external id is compared against the one the state already has, so rewriting a state with its own id is not a conflict.

Making a state the default clears the flag from every other state in the project — and the serializer does it during **validation** rather than on save, so it happens even when the save that follows fails.

Deleting refuses the default state and any that still holds work. The emptiness check counts through the plain manager, so an archived or draft issue keeps a state alive just as a live one does.

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

The guard reads both ways the Caddyfile cuts a path over. A named `path_regexp` matcher is the obvious one; the other is a bare path on the `reverse_proxy` line itself — `reverse_proxy /auth/* api-go:8000` — which is Caddy's own path matcher and cuts over just as completely. Reading only the first kind left sixty-five routes, the whole of `/auth/` and most of `/api/users/me/`, sitting behind this guard without it ever checking that Go served them. It does now, and Caddy's matcher semantics are written out rather than approximated: a star crosses slashes, and a path without one matches only itself.

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

## Migrated module: the page description, its versions and its duplicate

The five routes left on the page app: reading and writing the description, listing and reading its versions, and duplicating the page.

The description is a **file**, not a field. The `GET` streams the raw binary column back with a `Content-Disposition`, and a page that has none answers an empty file rather than a null or a `404`.

A locked or archived page refuses the write with a **numbered error code** rather than a message — `4701` and `4702`, the only place that shape appears in the migrated surface.

### The suspicious-content window is counted in characters

`validate_binary_data` decodes the whole document first, dropping what it cannot read, and only then takes **two hundred characters**. A byte window would be the obvious reading and it is wrong: a document full of multibyte characters is scanned further in than two hundred bytes would reach. A test builds a document where the two disagree.

The window also means a pattern that merely **straddles** the edge is missed, which is reproduced rather than tightened.

### The duplicate drops the collaborative binary

The copy carries the original's rendered description but not its binary, so the editor rebuilds the document from the HTML. It is linked into **every** project the original sits in.

And the asset copy is told the project the **loop variable was left holding** — the last of the page's projects, not the one the request named. A page in one project is unaffected; a page in several is not. Reproduced, with the reason in the comment.

The duplicate's response is read back through a queryset that annotates only the project ids, so its labels come back empty whatever the original carried.

### The version list and its detail

The list omits the documents; the detail carries all five of them. Both are scoped to a page with a **live** link to the project in the URL, which is what stops a version being read through a project the page was taken out of — GHSA-g49r and GHSA-ghcr.

## Migrated module: the issues inside an intake

The ten routes under `intake-issues/` and `inbox-issues/` — mounted twice, like the intake itself.

The status filter **defaults to pending alone**, so a caller that names nothing sees only what still needs triaging rather than everything that ever passed through. A filter of nothing but `null` narrows nothing instead of matching nothing.

### Two halves, separately gated

An update carries a work item and a link, and they are gated apart. A **guest may edit the work item**, and only its name and description at that — everything else they send is silently dropped rather than refused. The **link** moves only for someone above a member, or a workspace admin, which is what stops a guest accepting their own work item.

Accepting an issue that is still in triage moves it to the project's default state. A project with **no** default refuses the acceptance rather than leaving the issue stuck in triage, and the check runs before anything is written.

The status activity is sent **without a notification**, unlike the work item's, and both carry the intake link's id — the one activity keyword nothing else in the migrated surface sends.

### Deleting takes the work item too

Unless the issue was **accepted**: an accepted issue has left the intake and become ordinary work, so only the link goes. Every other status takes the issue with it.

### Small things kept

The priority is checked by hand before the serializer sees it, so an unknown one is a plain message rather than a field error. The create answers `200` rather than `201`. A project with no intake raises rather than answering, because the id is read off `.first()` with no guard. And the triage state is created on the spot when the project has none, so a project that has never used intake gets one the first time something lands in it.

The version task is handed the **previous** state on an update and the **request** on a create, which is what makes one a diff and the other a first version.

`skip_activity` together with a description change is how the migration tool writes without leaving a trail; anything else is recorded.

The description-versions routes under `intake-work-items/` and the public anchor routes stay on Django.

## Migrated module: the analytics summaries

`GET` on `default-analytics/` and on `project-stats/`.

The dashboard is ten numbers and lists over one filtered set of issues. The classification counts the **state group** rather than the row, so the bucket of issues with no state counts zero of them. Only the current year is charted, and the year is the server's rather than the caller's.

The three "who did the most" lists differ in more than their measure. Two keep five rows and the pending one keeps all of them; two exclude the rows with nobody attached and the **pending** one does not, so it carries a bucket whose user fields are all null. All three group by the rendered avatar as well as the four name fields, which Django does because a non-aggregate annotation joins the grouping whether it is added before or after the count — checked against the rendered SQL, where the two orderings produce the same five-column grouping.

The estimate sums read the issue's own `point` column rather than joining an estimate, and a set with nothing in it sums to **null** rather than to zero.

### Asking for nothing asks for everything

`project-stats/` intersects the requested fields with the valid ones, and an empty result is treated as asking for **all five**. Each is computed only when asked for.

The two issue counts go through the `issue_objects` manager and the other three do not — they count rows of their own table, which has no manager to apply. A bot is not a member for this purpose, and the completed count includes **cancelled** as well as completed, so an abandoned issue counts as finished here.

The three advance-analytics endpoints stay on Django.

## Migrated module: the analytics charts

`GET` on `analytics/` and on `saved-analytic-view/<uuid>/`, and with them `build_graph_plot`.

### A date axis becomes a month, and it is not padded

The four date axes are grouped by a year-and-month **string** built by concatenating two extracted numbers. March 2026 is `2026-3`, not `2026-03`, which matters to anything sorting the keys as text.

Each half is wrapped so a null becomes the empty string rather than a null — so an issue with **no** date does not get dropped by the null filter that follows. It lands under the key `"-"`.

### sort_data loses data on one axis

A **priority** axis is reported in a fixed order — low, medium, high, urgent, none — and every key that order does not name is **dropped**, not appended. Every other axis is sorted with the literal key `"none"` last and the rest in ordinary order.

The fixture is `sort_data`'s own output over ninety-two cases, including ones where the priority path drops a bucket. CI regenerates it.

### The grouping walks runs

`itertools.groupby` groups **consecutive** rows, and the dict comprehension around it keeps the **last** run when a key appears twice. The query orders by the dimension, so in practice a run is a whole group — but the Go port walks runs rather than collecting values, because collecting would quietly differ the day the ordering changed.

### The extras are not all filtered the same way

Each lookup table is filled only when the axis or the segment names what it describes. The **label** one goes through the plain manager rather than `issue_objects`, so a label is listed even when every issue carrying it is archived or a draft. The **assignee** one lists only people who have a picture at all — a filter the endpoint carries and not an obvious one.

### A saved view draws a different chart depending on the request

Its axes come from the saved view and its **segment comes from the request**, so the same saved view segments differently depending on what is asked alongside it. And its stored query is applied as it stands rather than reparsed, so a view saved before the filter grammar changed keeps whatever it stored.

The summary endpoints — `default-analytics/`, `project-stats/` and the three advance-analytics ones — stay on Django.

## Migrated module: saved analytics views and the export

The six routes under `analytic-view/` and `export-analytics/`.

### Every update clears the stored query

The serializer reads `query_data` on the update path — a key the model does not have and no caller sends — so the filters it derives from are always empty. It then derives them a second time with a method the parser does not know, which changes nothing since they were empty already. **The column comes back as the empty object whatever the request said.** The create path reads `query_dict` and works.

This is the `IssueView` serializer's quirk again, one table over, and worse: `IssueView` has a `save()` that recomputes the query correctly and papers over it, and `AnalyticView` has none.

The query itself is read-only and is derived from `query_dict` through the same **JSON-shaped** filter parser the project views use.

### The export validates and then forgets

`export-analytics/` checks the axes and does nothing with them itself — the task reads the body again. So the validation is about refusing a request early, not about what gets exported.

A segment is optional and an **empty** one is not a segment at all, so it skips the second check rather than failing it. The two allowlists are disjoint: nothing that can be grouped by can also be measured.

### No PUT

Django binds only `PATCH` on the detail path, so a `PUT` there is a `405`. Registering one in Go would be a new route rather than a port, and it is left off.

The chart endpoints — `analytics/`, `saved-analytic-view/`, `default-analytics/`, `project-stats/` and the three advance-analytics ones — all need the graph plot and stay on Django.

## Migrated module: webhooks

The seven routes under `webhooks/` and `webhook-logs/`. Admin only, at the workspace level.

### The secret is shown twice and no more often

On creation and on a regenerate. Everywhere else it is dropped, and the **only** thing dropping it is a context flag.

The `fields=` allowlists the views pass are dead on two independent levels — `DynamicBaseSerializer` discards the caller's list and overwrites it with `expand`, and the filter never *removes* anything even when it does receive one. So every route renders the whole model, and fixing either level alone would not make those allowlists confidential. The Go port renders the whole model too, with one flag for the secret, which is the behaviour rather than the intent.

### The url has to pass three checks, and they report differently

The scheme and the local-name check come from the field's own validators and answer under `url` as a **list**; the SSRF resolve and the disallowed-domain check come from the serializer and answer as a **string**. The frontend shows both, so both shapes are kept.

The local-name check looks at the whole netloc rather than the hostname, so `localhost:8000` passes it — the port makes the string stop matching. The SSRF check catches that host anyway.

A host on `WEBHOOK_ALLOWED_HOSTS` skips the **disallowed-domain** check as well as the SSRF one. It is already trusted, and the loop-back guard would only get in the way of a sibling service sharing a parent domain with Plane.

This is `internal/httpsafe`'s first caller; the package was written for the webhook delivery task and had none until now.

### Other

A repeat registration of the same url is a `409` rather than a `400`, because the unique index on the workspace and the url is the only one this table has. The token is a fixed prefix and a **version four uuid's hex with no dashes**, which a test checks nibble by nibble.

## Migrated module: the entity search

`GET` on `entity-search/`, which is what the editor's mention menu and the link pickers call. With this the whole search app is on Go.

It is the global search's smaller sibling and answers a different shape: each requested type is capped at a **count the caller chooses**, and the rows carry what a menu needs rather than what a search page does. A type nobody knows writes **no key at all**, so the caller gets a body without it rather than an empty list.

The count is parsed with `int()`, so a value that is not a number raises rather than falling back to the default.

### The two branches are not the same search

Everything is written twice, once for a request naming a project and once for one that does not, and the two copies differ in three places:

- The **mention** search changes its source table: the project's members inside a project, the workspace's outside one.
- The **page** search adds `is_global` outside a project and nowhere else.
- The project branch's mention search is made `distinct` and the workspace one is not.

Pages are offered only when **public** in both branches, and their project id comes from the join rather than from the page — so a page in several projects appears once per project.

### The project search does not narrow by scope at all

It is the same query in both branches, and it offers a **public** project whether or not the caller is in it. The membership it does check is not required to be **active**, unlike every other search here.

The issue search here also drops the archived-project filter the global search carries.

## Migrated module: the global search

`GET` on `search/`, which runs up to eight searches side by side and returns them under one key each.

**An empty search is not a search for nothing.** The query is passed as `query or None`, so an empty one reaches each filter as nothing at all, every `if query` is skipped, and the endpoint answers with everything the caller can see. An entity nobody knows is dropped from the `entities` list rather than refused.

### The workspace search is not scoped to the workspace

It answers with every workspace the caller belongs to, whatever slug they asked under, and it does not check that the membership is **active** either. The other seven are all scoped to the slug and to active project membership.

### The intake search is the issue search's twin, and differs in two ways

It goes through the **plain** manager rather than `issue_objects`, so an archived, draft or triage issue is eligible — which is the point, since everything in an intake is in triage. And it keeps only what is still **waiting** in or **snoozed** inside an intake.

Both are capped at a hundred rows. The other six are not capped at all.

Pages project **lists** rather than single values, because a page reaches its projects through a link table and can sit in more than one.

The entity search under `entity-search/` is a separate endpoint and stays on Django.

## Migrated module: the issue search

`GET` on `search-issues/`, the picker behind every "link this to something" box: choosing a parent, a related issue, a sub-issue, or an issue to put in a cycle or a module.

Each of those asks for a different set of exclusions, and the switches that turn them on are read as **strings** rather than booleans — so the value has to be exactly `true`. `TRUE`, `1` and `yes` all read as off.

`target_date` is the odd one out. Its default is the Python boolean `True` rather than a string, and the filter tests for the word `"none"`, so the default can never match it and the filter is off unless a caller names it.

The search itself looks in three places at once — the name, the project's identifier, and the sequence number. The sequence branch is skipped entirely for a query over **twenty characters**, so pasting a long string cannot turn into a numeric scan. It takes **whole** numbers only: `PROJ-42` finds 42, `abc42` finds nothing, and `v1.2` finds only the 2, because `v` is a word character and there is no boundary before the 1.

The guest narrowing here has no escape hatch — `guest_view_all_features` does not reach this endpoint, unlike the view and page lists — and the membership it checks is not scoped to the workspace either.

The workspace-wide searches under `search/` and `entity-search/` are a different endpoint and stay on Django.

## Migrated module: the intake itself

The ten routes under `intakes/` and `inboxes/`: the queue a project's untriaged work lands in.

**The same viewset is mounted twice**, under its current name and the one it had before intake was called inbox. Both are live, both are served, and a test counts the routes on each so renaming the feature in one place does not quietly drop the other.

The list route is not a list. It serializes `.first()`, so the body is one object rather than an array — and a project with no intake gets the **empty object**, because a serializer handed nothing renders nothing rather than failing.

Deleting the intake a project falls back to is refused. An intake that is not there at all answers `500` rather than `404`: Django reads `is_default` off the result of `.first()` with no guard.

The pending count is the one status the triage board treats as waiting, and the project and workspace are read-only, so an intake cannot be moved between projects.

The issues inside an intake are a separate viewset and stay on Django for now, along with the public anchor routes that reach them.

## Migrated module: pages

Thirteen routes: the list, the summary, create, retrieve, update, delete, lock and unlock, access, archive and unarchive, and the two favourite ones.

A page belongs to a **workspace** and reaches its projects through a link table, so the same page can sit in more than one. The list shows only pages with **no parent** — a child is reached through its parent — and only those the caller owns or that are public.

### Zero is the public page

The access column reads the opposite way round from every other flag in this codebase: `0` is public and `1` is private. Pinned by a test, because it is exactly the sort of thing that gets "corrected" later.

### The two halves of one button sit behind different permissions

`ProjectPagePermission` keys its role rules on the **HTTP method** rather than on the action. Locking a page is a `POST` and needs a member; **unlocking it is a `DELETE` and needs an admin**. Same button in the interface, two different rules behind it. A guest may read and nothing else.

And a **private** page is readable by its owner alone — the community implementation of the private-page hook returns false for everyone else, whatever their role.

### Archiving and its timestamp

Archiving walks the page and its descendants in one recursive statement and clears **every** member's favourite of it. The body reports `str(datetime.now())`, which is a naive **local** wall clock with a space where the `T` would be, rather than an ISO instant — and it reads the clock a second time, so the value in the body is not quite the one in the column. The column itself is a `DateField`, so the page list reports a day rather than a moment.

Unarchiving cuts the page loose from a parent that is still archived, rather than leaving it under something invisible.

A page has to be archived before it can be deleted. Deleting cuts its children loose, clears the favourites, and removes the recent visits for good rather than marking them deleted.

### Small asymmetries kept

The update route answers **the same message for every failure inside its try block** — including a parent that does not exist, which has nothing to do with ownership. The access route reads the request twice with different defaults: it refuses only when the caller named an access that differs from the page's, while the value written falls back to public, so a request naming nothing makes the page public. And the summary's queryset has no `deleted_at` filter on the project link where the list's does, so a page whose link was removed is still counted.

The description, the versions and the duplicate route are not migrated yet; they are separate paths, so the matcher stops before them.

## Migrated module: notifications

The ten routes under `workspaces/<slug>/users/notifications/`: the list, the unread counts, mark-all-read, and the six that act on one notification.

Only **issue** notifications appear at all — the queryset pins `entity_name` — and they are ordered by when they wake from snoozing before how recent they are.

### The same notification has two shapes

The list annotates three read-only flags; no detail route does. DRF skips a read-only field whose attribute is missing rather than failing, so a notification read one at a time carries **three fewer fields** than the same notification inside a list. Two of those three, `is_inbox_issue` and `is_intake_issue`, are the same subquery under different names.

### Asking for `mentioned=false` asks for the mentions

The parameter is read as a string and every non-empty string is true in Python, so **any** value turns the mention filter on. Only leaving the parameter out turns it off.

### The snoozed filter's two halves are not two halves of a whole

`snoozed=true` is `snoozed_till < now OR snoozed_till IS NOT NULL` — the second half subsumes the first, so it really means "has ever been snoozed". A notification snoozed until tomorrow satisfies **both** branches. Kept as written.

And the pair is looked up in a two-key dictionary rather than tested, so `snoozed=maybe` is a `KeyError` and a `500`. The mark-all-read route reads the same two flags for **truth** instead, so there any value works and an unknown one is not an error. Two routes, same two names, different rules.

### A guest asking about what they created gets nothing

Not nothing *from that set* — nothing at all. The branch replaces the whole queryset, so the other types the caller asked for go with it.

The `type` parameter is a comma-separated **set** on the list, unioned; on mark-all-read it is a single choice, and its name for the subscribed set is `watching` rather than `subscribed`. That is also the one place the subscription is counted without first asking whether the person made or was given the issue.

### Paging

The list is paged only when the caller sends **both** `per_page` and `cursor`. Naming one of them alone returns everything, unwrapped.

The update route reads exactly one field out of the body and builds its own payload, so everything else a caller sends is dropped — and a request that names nothing still **clears** the snooze, because the payload puts a null there rather than leaving it out.

The notification preferences under `users/me/notification-preferences/` stay on Django: they belong to the user app rather than to this one.

## Migrated module: workspace views

The six routes under `workspaces/<slug>/views/`: the views that belong to no project.

The project list's twin, differing in three ways. There is **no favourite flag**, because nothing annotates one here. The guest narrowing has **no escape hatch** — a workspace guest sees only their own views, whatever the projects are configured to allow, where a project guest can be let through by `guest_view_all_features`. And the order is the caller's to choose.

`sanitize_order_by` is ported with it: at most one leading dash is stripped, the bare name is checked against an allowlist of three fields, and anything else falls back to `-created_at`. A doubled dash is rejected rather than reaching the ORM, which is what the function exists for.

The retrieve carries **no permission decorator at all**, so the viewset's own default applies and any signed-in user reaches it. The queryset still hides a private view they do not own, and the serializer is then handed the nothing that comes back, which renders as an empty body rather than a `404`. The visit is recorded either way, with no project to record it against.

Deleting clears the view's favourites but, unlike the project route, **leaves the recent visits alone** — a deleted workspace view keeps showing up in the recent list until something else clears it.

The workspace issue list behind these views, `workspaces/<slug>/issues/`, stays on Django for now: it needs the grouped paginator over every project the caller can see.

## Migrated module: project views

The nine routes under `views/` and `user-favorite-views/`: list, create, retrieve, update, full update, delete, and the three favourite ones.

A view is visible to its owner and, when its access is public, to everyone. On top of that a **guest** sees only their own unless the project has been opened up with `guest_view_all_features` — a second narrowing rather than a replacement for the first.

The retrieve checks the guest rule **after** reading the view rather than folding it into the query, so a guest asking for someone else's view is told no rather than told it does not exist. A view that is not there at all answers `500` for a caller the rule applies to and an empty body for everyone else, because Django reads the owner off the result of `.first()` with no guard and the expression short-circuits before reaching it.

Updating is refused for a locked view and for anyone who is not the owner — both as a plain `400`, not as a permission failure. Deleting wants an admin or the owner, and clears the view's favourites and its recent-visit rows; the visits go for good rather than being marked deleted.

The list's `fields` parameter narrows what each view renders. An empty entry is dropped, so a trailing comma does not ask for a field with no name.

### The query column, and the second POST shape

`query` is derived from `filters` on every save and never accepted from the caller. Nothing in the backend reads it back — it is returned in the body and that is all — but it has to be written the way Django writes it.

That turns out to need a **second** filter shape. The issue lists parse a query string, where every value is text; this path is handed decoded JSON, where a value is usually a list and occasionally a string, and the POST branch stores it **as it arrived**. The difference is visible: a string date is iterated one character at a time and a list of the same dates one clause at a time. Both are upstream, both are reachable from this one endpoint, and the fixture covers both.

`viewQueryFromFilters` is that shape, diffed against `issue_filters(filters, "POST")` over 53 blobs. The GET path is untouched.

An update that does not mention `filters` **clears the stored query**, because the model recomputes it from whatever the instance now holds rather than from what the request named.

And a filter carrying a relative date term — `2_weeks;after;fromnow` — cannot be saved at all. It resolves to a `datetime.date`, the column is a plain `JSONField`, and `json.dumps` refuses a date, so the request answers `500`. Nothing rewrites the value on the way in and nothing catches the error on the way out. One fixture row covers it.

The serializer computes the query twice on update, the second time with a method the parser does not know — but the model runs last and always parses as a POST, so the serializer's version never reaches the column and there is nothing to reproduce.

## Migrated cycle: transferring issues to another cycle

`POST` on `cycles/<uuid>/transfer-issues/`, the last of the cycle app's own routes.

The endpoint is more than an update because of what it has to freeze. Once the issues are gone the old cycle can no longer be measured, so everything its board would have shown — the six counts, both distributions, both burndowns — is computed first and written into `progress_snapshot`. That is the record the progress and analytics endpoints read from then on, which is why those two branch on it.

Naming a destination cycle that does not exist answers `500`, not `400`: Django reads the end date off the result of `.first()` with no guard. A destination whose end date has passed answers `400`, and a missing source answers `400`.

### Three ways to count the same cycle

The snapshot's six counts are **not** the cycle list's and not the analytics endpoint's. They count **links** rather than distinct issues, and they exclude only what their filter names — deleted, archived and draft issues and dead links. A triage issue, or one in an archived project, is counted here and excluded by the `issue_objects` manager that the distributions beside it go through.

And the frozen distribution counts `id` where the analytics endpoint counts the **grouping column**. So the bucket holding the issues with no assignee is counted properly in the snapshot and reported as zero by the live endpoint, for the same cycle. Two blocks that read alike and do not agree; both reproduced, and a test states the difference.

### What moves

Only backlog, unstarted and started work. A completed or cancelled issue stays with the cycle it was finished in, and an issue with **no state at all** moves nowhere, because the filter is a join to `states` rather than a test on the column. A soft-deleted issue's link does move, since only the link's own deletion is checked.

Only the cycle column is written. A bulk update touches the named column and nothing else, so the links keep the timestamps and the author they had.

The snapshot and the move are two statements, not one transaction. Django runs this view in autocommit — `ATOMIC_REQUESTS` is not set — so a failure between them leaves the snapshot written and the issues where they were.

The single activity it sends names **no issue**: the move is about the cycle, and the task fans it out over the list it carries. `issue_id` therefore has to be `null` rather than the empty string, which is what `nullableID` is for.

### GORM drops a second anonymous struct

The obvious row for this query — the cycle embedded beside a struct holding the six counts — parses to the cycle alone. The fields under the second embedded struct get **no column, scan no value and raise no error**, and the `embedded` tag does not help. Nothing in the library says so. The counts are therefore declared inline, and a test pins all three shapes: two embedded structs, the second one tagged, and an embedded struct beside a plain field, which is what every annotated row in this package relies on.

It is the same family of silent failure as a struct embedded two levels deep, which cost a CI run earlier in this migration. Second time, different shape.

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

`modules/<uuid>/issues/<uuid>/` is cut over with all four of its methods, and three of them answer a 500 — which is what Django answers too. `DELETE` is written by hand and works. `GET`, `PUT` and `PATCH` fall through to DRF's own generic actions, and a generic action finds its row through `get_object()`, which looks for a URL keyword called `pk`. This path has none: its keyword is `issue_id`. So `get_object` fails its own assertion before touching the database, and the base view turns anything it does not recognise into one sentence and a 500.

It is the same answer for a work item that exists, one that does not, and one in another project, because nothing is ever looked up. `cycle-issues/<uuid>/` is the same call with the same three methods. Both were held back for a long time on the grounds that cutting a path over means owning every method on it — which is exactly why they are cut over now: owning a method includes owning the answer it already gives.

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

## Shared: the work item save path

`internal/issues` holds what `Issue.save` does, which is the part of a work item neither API writes for itself. Both APIs create and update work items through a serializer, and a serializer's `save()` ends in the model's: that is where the sequence number is handed out under an advisory lock, where the sort order is derived from the work items already in the chosen state, where a missing state is filled in, where `completed_at` follows the state, and where the plain-text copy of the description is made.

None of it is reachable through a serializer field, and all of it has to happen the same way on both sides while the two run together — a sequence number the two halves disagree about is two work items with the same key. `AdvisoryLockKey` is `convert_uuid_to_integer`, the first eight bytes of the project id's SHA-256 read as a **signed** integer; `testdata/advisory_lock_keys.tsv` is ten of Django's own keys, half of them negative, because reading the digest unsigned would lock on a different number and lock nothing at all against the other half of the deployment.

## Shared: the lxml round-trip

The external API's work item serializer parses `description_html` with `lxml.html.fromstring` and writes it back with `tostring` before the sanitizer ever sees it, so what gets stored is libxml2's reading of the markup rather than the markup the caller sent. `internal/htmlsanitizer.RoundTrip` is that pass, and `testdata/lxml_fragment.tsv` is what lxml really writes for sixty-five fragments, generated rather than written by hand.

`fromstring` does not return a fragment. It parses a whole document and then decides what to hand back: the single element the body holds, or the body itself retagged as a `div` or a `span` depending on whether a **block-level** tag is anywhere inside it, or — when nothing reached the body at all, as for a lone `<script>` or `<title>` — the document. So `<p>a</p><p>b</p>` comes back wrapped in a `div` and `<span>a</span><span>b</span>` in a `span`, and plain text comes back as `<span>plain text</span>`.

Markup that parses to nothing is a `ParserError` rather than an empty string, which is how `description_html: ""` comes to be **refused** with `Invalid HTML passed` rather than stored. A comment on its own is refused the same way.

Two details of the writing are libxml2's rather than the HTML specification's. A void element is written without a closing tag or a slash, so `<hr/>` comes back as `<hr>`. And an attribute value holding a double quote and no single quote is written in single quotes rather than escaped, so `title='he said "hi"'` survives as it was written.

The one place the Go port does not follow lxml is the `tbody` an HTML5 parser implies around a table's rows, which libxml2 never inserts. Whether the element was implied cannot be read off the parsed tree, so it is read off the markup: a fragment that never says `tbody` cannot have one in its output. A fragment that says it for one table and leaves it out of a second keeps both — markup no editor produces.

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

## Migrated module: the rest of a person's own routes

Seven routes are implemented and cut over: the notification preferences and their edit, the caller's own activity feed, the workspace they were in last, and the three graphs the home screen draws. They live in `internal/project` rather than `internal/user` because five of them read work items or work item activity.

**The preference row is read with a `.get()`.** Somebody who has a workspace-level preference row as well as their own gets a 500 rather than either of them, and somebody with none at all gets a 404 rather than a set of defaults.

**The caller's own activity feed narrows to nothing.** Not to a workspace, not to projects they still belong to, and not away from the four fields the per-workspace feed hides — so somebody who has left a project still sees what they did there, comments and reactions included.

**The workspace they were in last** answers with an empty pair rather than a 404 when they have never been in one. Its memberships are not narrowed to the active ones, so a project they were removed from is still listed. The workspace object carries fifteen fields rather than the seventeen the serializer declares: `total_members` and `role` are annotations, nothing annotated them here, and DRF leaves an absent attribute out rather than failing.

**Every dashboard number reaches the caller's work through an inner join on the assignee link, and that join does not check whether the assignment was taken back** — so a deleted assignment still counts. The numbers also narrow to the workspace alone rather than to the projects the caller belongs to, which is the opposite of what the profile page does with the same question.

The completed-work graph buckets by the calendar week of the year **taken modulo four**, so two weeks nine apart share a bucket; the dashboard's own weekly split is a real week-of-month. Both read the month off the query string and default to January rather than to this month.

## Migrated module: one person's work item list

`GET /api/workspaces/<slug>/user-issues/<id>/` is implemented and cut over. It is one person's work across the workspace — everything assigned to them, raised by them, or that they are following — and it takes both filter languages and the grouped and sub-grouped paginators.

**The three ways in are ORed.** A work item counts as somebody's if they have it, raised it, or follow it. Django reads that as an id list rather than as a join, which is why a work item assigned to them twice is still one row.

**The narrowing is by the caller, not by the person being asked about.** The list is cut to projects the caller belongs to, so two people looking at the same person's list see different work items.

Two pieces of shared machinery were widened rather than copied. The list scope now leaves the project condition off when there is no project, which is what makes a workspace-wide list possible at all; and the group value lists read the workspace's own rows in that case. The assignee group is the only one where that is a different **table** rather than the same one unnarrowed: a project's list of people is its membership, a workspace's is the workspace's. There is a test pinning both.

## Migrated task: the notification email

`send_email_notification` now runs on the Go worker, which finishes the notification chain: the history is written, the notifications are made, the batches are stacked and the email is rendered and sent, all on the Go side.

**It sends nothing unless somebody looked at the work item recently.** Every link in the email is built from an origin the activity task parks in Redis for ten minutes. With no origin there is nothing to link to and the task returns — so a change made by a client that sent no origin, or one whose email is stacked more than ten minutes later, is silently not emailed. Reproduced rather than corrected.

**The lock names the whole batch.** It is built from every notification id the stacking handed over, sorted, which is why two emails to the same person in one sweep take the same lock and only the first goes out.

Two more reproduced rather than corrected:

**The time of day is written on the twenty-four hour clock with an am or pm after it**, so an afternoon change reads `14:05 PM`.

**The avatar url is the origin and the stored value glued together**, so somebody whose avatar is a full url elsewhere gets a broken image in the email.

And one that is upstream's and quietly costs a message: a failure to send releases the lock so the batch could be tried again, but the stacking has already marked it processed, so nothing ever will.

## Migrated module: the Django template subset

`internal/djangotemplate` renders the notification emails' templates, and the templates themselves are **copied from `apps/api` unchanged**.

Translating them into Go's own template syntax was the other option and it was rejected for a reason worth stating: a translated template drifts. Somebody changes a line of the html on the Python side, the two copies stop agreeing, and nobody finds out until a customer reads an email that is missing a row. Keeping the file byte for byte means an upstream change is a copy rather than a re-translation, and a diff tells you whether the two are the same. CI checks both: that the copy is the file `apps/api` ships, and that rendering it here gives what Django gives.

The subset is what those templates use and no more — variables with dotted paths and numeric indices, `if`/`elif`/`else`, `for`, the operators `and`, `or`, `not`, `==`, `!=` and `>`, and the filters `length`, `add`, `slice`, `last` and `safe`. Anything else is refused at parse time rather than rendered wrongly.

The corpus found three things a careful reading had already got wrong:

**A missing key writes nothing; a key holding null writes the word.** Django draws that line with `string_if_invalid`, and the templates lean on it — several of their numbers come from a context that does not always carry them, and `There are  new updates` is what upstream sends today.

**A numeric step into a string takes that character.** That is how the templates write somebody's initial when they have no avatar.

**Escaping uses the hexadecimal entity for an apostrophe**, where Go's own escaper uses a decimal one. A subject line with an apostrophe in it would otherwise differ from Python's byte for byte.

## Migrated task: the email stacking

`stack_email_notification` now runs on the Go worker. It is the five-minute sweep that reads every notification nobody has been emailed about yet, groups it by who is to be told and about what, and queues one email per pair. The email itself — `send_email_notification`, which renders a two-hundred-line template — still runs on the Python worker.

**Every email a person is queued carries all of that person's notification ids**, not just the ones about the work item it is for. The list is built across the whole of a person's notifications and then handed to each of their emails, so the first one sent marks the rest as sent — and the lock each email takes is built from that same list, so two emails to the same person in one sweep take the same lock and only one of them goes out. Reproduced rather than corrected: this is why somebody with changes on three work items gets one email rather than three, and why which of the three they get is whichever the worker picked up first.

## Migrated task: the webhook fan-out

`webhook_activity` now runs on the Go worker, which completes the webhook chain: the Go side now works out what changed, picks the webhooks that asked about it, serialises the object, delivers it and logs the result.

It needed the v1 API's nine serializers, and two things about them decide what a receiver sees.

**The annotations are absent.** The task reads each model with a plain lookup and no annotations, and DRF leaves a field out rather than failing when the attribute is not there. So a project in a webhook carries no member count, a cycle no work item counts, a module neither its counts nor its members, and a link between a cycle and a work item no sub-item count — even though all four serializers declare them. The same reading is why a work item carries no cycle and no module: both are declared through a reverse relation that has no single value.

**The key order is the serializer's**, and a receiver sees the bytes. Each object is written by hand rather than through a map, which has no order; the payload's two halves are then spliced into the delivery as they were built.

Two behaviours are worth knowing:

**A delete carries only an id.** Every other verb reads the object back out of the database and sends it in full; a delete cannot, because the row is gone.

**An intake work item reaches every webhook.** There is no switch for it — the filter narrows only for the seven events that have one — so a webhook that asked for nothing at all still hears about one.

## Migrated task: the webhook delivery

`webhook_send_task` now runs on the Go worker. It is the last link in the webhook chain: one webhook, one event, delivered and logged. The middle link — `webhook_activity`, which picks the webhooks that want an event and serialises the object for them — still runs on the Python worker, because it needs the nine v1 serializers and those are the next piece.

**The payload keeps the bytes it was given.** The two halves that came from a serializer are spliced in as they arrived rather than decoded and rebuilt, so a receiver sees the same key order the sender produced. The rest of the object is written by hand in the order it is documented in rather than through a map, which has none.

**The signature covers exactly the bytes that are sent.** That is what lets a receiver check it, and it is why the whitespace the two languages choose does not matter: each side signs what it sends.

**A url that turns out to point somewhere internal is not a failure.** It is logged with a 400 and dropped — not retried, and not grounds for switching the webhook off. The cause may be a transient answer from dns, and switching a customer's webhook off for that would be worse than missing the event. Every delivery goes to an address that was resolved and checked first and is then connected to as a literal, so no second lookup can happen in between; redirects are never followed.

**The backoff is not exponential.** Celery is asked for a doubling one starting at ten minutes, and its ceiling defaults to the same ten minutes — so every wait is a uniformly random stretch between none at all and ten minutes. After five of them the webhook is switched off and whoever made it is emailed.

One difference in where the waiting happens: Celery publishes a new message with an eta and holds it in the worker until then, and this holds it in a timer. With the default acknowledgement settings neither survives the worker stopping.

## Migrated task: the notifications

`notifications` now runs on the Go worker. It reads the history the previous task wrote and decides, for each person who cares about the work item, whether they hear about it in the app and whether they are sent an email.

**Thirteen activity types pass through silently.** Being added to a cycle or a module, a reaction, a vote and anything to do with a draft produce history but never a notification.

**It corrects a gap this migration opened.** The activity task publishes the rows it wrote, and until now it published them without `issue_detail`. The notification task reads `issue_detail.id` to tell a line about *this* work item from a line about the other side of a relation, so reading it off nothing raised and the Python task quietly wrote no notifications at all. The activity task now carries that object; only its id is read, which is why the serializer's other nested details are not built.

Three upstream bugs are reproduced rather than corrected, and all three are worth knowing because they otherwise read as faults in this port:

**A mention email goes to the wrong reader.** The loop that emails the people named in a description reads `subscriber` — the variable the *subscriber* loop left behind — rather than the person it is writing about. So the email about somebody being mentioned is addressed to the last subscriber who was notified.

**And when there were no subscribers, nothing is written at all.** In that case the leftover variable still holds the task's own `subscriber` flag, which is a boolean, and writing a boolean into a uuid column fails the whole insert. Since both inserts happen at the end, the notifications are lost with the emails.

**The collapsed-description branch reads another leftover.** When the last line of history was a description change by the same person, the mention notification takes its two identifiers from the subscriber loop's last activity. If that loop never ran there is no such variable, and Django raises a `NameError` that loses everything.

One more: the people named in a description become subscribers, but the check that decides whether they need to be does four separate lookups per person and none of them is narrowed by the mention being valid — so an id in the html that is not a person is simply skipped by the last of the four.

## Migrated task: the work item history

`issue_activity` now runs on the Go worker. It is what writes every line of a work item's history, and nothing else in the worker is queued as often. All twenty-seven activity types move at once, because the routing is by task name — taking half of it would have left the other half writing nothing.

**Three things happen before any history is written.** A project id that is not a uuid ends the task silently. The request's origin is parked in Redis beside the work item for ten minutes, which is what lets the notification emails build their links. And the work item's `updated_at` is touched, so a change to something hanging off it still counts as touching it.

**A failure loses the whole batch, not the line that failed.** Django lets a missing row reach the task's own except, which throws away every activity the request would have written. Four places reach it, and all four are reproduced: a label or an assignee that has since been deleted, a cycle in a create record that has gone, a comment reaction whose row is not there, and — the one worth knowing — **clearing an estimate**. The field name is built from the *new* estimate's type, and with nothing to move to there is no new estimate to ask; so clearing an estimate writes no history at all, not even for the other fields that changed in the same request.

**The first line of a history is never the one that was queued.** The create row is written on its own and then rewritten: its timestamp becomes the work item's own and its actor becomes whoever raised it, whoever queued the task and whenever it ran.

**A run of description edits collapses into one line.** When the line before was also a description change by the same person, that line's timestamp is moved to now instead of a new one being written.

**A state id that is not one reads as no state; a parent id that is not one abandons the field.** Two trackers, two answers to the same question.

**Both names for a field reach the same tracker.** The session API sends `state_id` and the external one sends `state`; a payload naming both is walked twice and writes the change twice.

A few more that are reproduced rather than corrected: a reaction is found by reaction, project and actor without naming the work item, so somebody who left the same reaction twice has the line point at one of them arbitrarily; deleting a relation names the far side's field by hand and only turns `blocking` and `blocked_by` around, so deleting a `start_after` leaves both lines saying `start_after`; a deleted draft's line names no work item at all; and the comment on a cycle move carries a newline and a run of spaces, because upstream builds it from a string laid out across two lines.

The trackers that need no database are checked against a truth table generated from the real ones, and CI regenerates it.

## Migrated task: the first link in the webhook chain

`model_activity` now runs on the Go worker. It is the task the Go API already queues after every tracked save, and its whole job is to work out what actually changed and fan one `webhook_activity` out per changed field. The two links after it — picking the webhooks that want the event, and delivering to them — still run on the Python worker.

Two things about the comparison decide what a webhook ever hears:

**A create is not a comparison at all.** No previous state means one activity with the verb `created`, no field named and no values; the next link fills in the whole object from the database.

**An update only looks at keys the previous state also had.** A field the request introduced that the snapshot never carried is not a change as far as this is concerned — the snapshot is what the model looked like, and a key it lacks is a key the model does not have. So **adding a field to a serializer makes its first write invisible to webhooks**, and stays invisible until something else writes it a second time. Reproduced rather than corrected.

Values are compared as the json they decode to, so a number and its string are a change and two objects with the same pairs in a different order are not.

One difference with no behavioural weight: the changed fields go out in a fixed order rather than the request's own. A Go map has no order to walk, and a stable one is what makes the stream readable.

## Migrated task: the two nightly sweeps

`delete_old_s3_link` and `archive_and_close_old_issues` now run on the Go worker. Both are daily jobs the Go beat already schedules.

**An expired export loses its link, not its history.** The spreadsheet goes out of the bucket and the row keeps everything but its url — so the history still says an export was made and what it was called; it just no longer offers a way to fetch it.

**Archiving and closing share a schedule and nothing else.** One takes work items that have been finished and left alone for as long as their project asks; the other takes ones still open and moves them into the project's default state. A work item is only eligible when nothing it belongs to is still running.

Three things about that eligibility are worth knowing, and all three come from Django joining the links rather than asking about them one at a time:

- **One finished module is enough.** The rule is "it is in none of these, or it is in one that has finished", not "every one it is in has finished". A work item in one module past its target date and another still running is archived.
- **A work item in three finished modules gets three activity rows.** The queryset is not made distinct, so it contributes one entry per matching link. The row itself is only written once; the history is what shows the repetition. Reproduced rather than corrected.
- **Having no intake row counts as decided**, the same as being accepted, declined or marked duplicate.

**A project with no default state closes its work items into whatever cancelled state comes first anywhere in the installation.** The lookup is scoped to neither the project nor the workspace. Reproduced rather than corrected: correcting it would move work items into a state this port chose, which is a different outcome rather than a fixed one.

One deliberate difference: an object the bucket will not remove is logged and the sweep carries on. Django lets that raise out of the loop, which leaves every later expired link in place.

## Migrated task: the work item link crawler

`crawl_work_item_link_title` now runs on the Go worker. It goes and looks at a link somebody attached to a work item so the list can show its title rather than its address, and it keeps the site's icon beside it.

**A link somebody pasted is an address this server will go to**, so the whole of the care here is about not being talked into going somewhere internal. `internal/httpsafe` already had the pinned POST the webhooks use; this adds the GET and the redirect follower. Every hop resolves the host, checks every address it resolves to, and then connects to the **validated address literal** — the hostname is still used for the `Host` header, TLS SNI and certificate verification, but no second lookup happens, so the address that was checked is the address that is reached. A redirect is never followed by the client; the chain is walked by hand, up to five hops, re-resolving and re-checking each one. A declared favicon is checked on its own account, because a page can point its icon somewhere the page itself was not.

**Nothing here fails the delivery.** A site that is down, refuses, or turns out to be internal leaves a null title and the fallback icon, which is what the row ends up holding. That fallback is the same bytes the Python task carries, so a link with no icon looks the same whichever worker crawled it.

**Crawling a link forgets who added it.** Django saves the whole link row and `BaseModel.save` blanks the two audit columns, because the worker has no current user. Reproduced.

One deliberate difference: the body is read up to five megabytes rather than without limit. Django reads whatever arrives; a page that never ends would fill the worker rather than time out, and the timeout is one second.

## Migrated task: the two that look after an uploaded file

`get_asset_object_metadata` and `delete_unuploaded_file_asset` now run on the Go worker. The first is queued by the Go API when an upload is confirmed; the second is a nightly one the Go beat already schedules.

**The metadata keeps boto's field names**, not this client's. The rows already in the table were written by boto and the web app reads them by those names, so the etag is quoted the way the header arrives — the Go client strips those quotes and they are put back — and the user metadata keys are lowercased the way boto lowercases them.

**A bucket that refuses the read is not an error.** Django logs it and writes null over whatever was there, so an object that has since gone takes its metadata with it. That is reproduced.

**The sweep is a soft delete.** It is a queryset delete, so the rows stay and are marked, and nothing goes to the bucket — there is nothing there to remove, which is the whole point of the sweep. Its window is refused when negative, for the same reason the hard delete's is: Django reads it with a bare `int()` and a negative value would put the cutoff in the future and sweep away every asset currently waiting to be uploaded. Zero is a real window and is taken.

## Migrated task: the three that keep a description's history

`page_transaction`, `track_page_version` and `issue_description_version_task` now run on the Go worker. All three were already queued by the Go API and answered by the Python one.

**The page log almost never gets written.** `page_transaction` logs one row per component a description gained, and an image component's row puts the image's `src` into `entity_identifier` — a column that is a uuid. A source that is a url raises before anything is written, the task swallows it, and neither the insertions nor the deletions happen. So a page with a single image logs nothing about its mentions either. Reproduced rather than corrected: correcting it here would write rows the Python worker never wrote, which is a data difference rather than a behaviour one.

The two version tasks look like the same task and are not:

- **The page's rewrite does not move `last_saved_at`.** A run of quick edits keeps folding into the version the first of them made, and the ten-minute window is measured from that first edit rather than the last — so a long session can fold an hour of work into one version. The work item's copy does move it.
- **Only the page's is capped.** It trims to twenty; the work item's keeps every version it makes.
- **Only the page's recomputes the stripped copy.** Its model's `save` derives it from the html; the work item's carries across whatever the work item already had.

Both write their two audit columns empty whatever they were built with, because the worker has no current user and `BaseModel.save` blanks them.

## Migrated module: the workspace work item list

`GET /api/workspaces/<slug>/issues/` is implemented and cut over — every work item in the workspace the caller can see, across all their projects. It is the first route to use `internal/complexfilters`, and this piece adds the SQL half of that package.

**It takes two filter languages at once.** `?filters=` is the JSON tree; the rest of the query string is the older flat one every other list takes. Both are applied and ANDed together.

The SQL half is where the JSON tree stops being an abstract shape:

**A relation is joined once however many conditions name it.** That is what Django does within a single `filter()` call, and it is why `{"and": [{"assignee_id": a}, {"assignee_id": b}]}` asks one row to be two people at once and therefore matches nothing. Reproduced rather than corrected.

**The same relation under an `or` becomes an outer join.** An inner one would drop the work items the other branch of the `or` is there to find, so a relation named anywhere beneath an `or` is joined outer even where it also appears under an `and`.

**A negated relation stops being a join at all** and becomes a pair of correlated `EXISTS` subqueries — which is what keeps "not assigned to this person" from quietly meaning "has some other assignee". The soft-delete companion is written as a left join inside its own subquery, so it is also true for a work item with no rows on the far side; that shape is Django's, oddity included.

**An empty `IN` is an empty result** rather than a syntax error, and a range with a number of ends other than two is a `ValueError` inside the ORM, which answers 500 rather than 400.

The list's own projection differs from the project list's in one way worth knowing: its module ids come from the prefetched links rather than from an aggregate that joins the module, so a module that has since been archived is still reported here and is not there.

## Migrated module: the complex filter backend

`internal/complexfilters` is the JSON filter tree the two cross-project work item lists accept — `?filters={...}`, nested `and`, `or` and `not` around leaf objects of field lookups. The package parses, validates and evaluates it into the same Q tree Django builds. Nothing calls it yet; the two lists that will are the next piece.

It was built against a truth table generated from the real backend rather than from a reading of it, and the table found five things that reading would not have:

**A JSON list on an `__in` filter keeps only its last item.** The leaf is written into a QueryDict with `setlist` and then read back with `get`, which takes the last value. So `{"priority__in": ["high", "urgent"]}` filters on urgent alone, while `{"priority__in": "high,urgent"}` — the same thing as a comma-separated string — filters on both.

**A capitalised operator silently filters nothing.** The structure check lowercases the key before deciding it is an operator, and the evaluator does not, so `{"OR": [...]}` passes every check and is then read as a leaf where no field matches.

**`"1"` and `"0"` are not booleans.** The boolean widget's table maps `"True"`, `"true"` and `"2"` to true and `"False"`, `"false"` and `"3"` to false; everything else, `"1"` and `"0"` included, becomes null. For `is_draft` that writes `is_draft IS NULL`; for `is_archived`, which runs through a method, it writes no condition at all.

**The two range filters are not the same filter.** `start_date__range` and `target_date__range` insist on exactly two dates; `created_at__range` and `updated_at__range` take however many they are given, including one or three.

**An empty Q combines away rather than wrapping.** That is why `{"or": [x]}` comes back as an AND of one thing rather than an OR, and it is visible in the SQL.

## Migrated module: the project advance analytics

The three project-scoped routes are implemented and cut over. They read the same filters as the workspace ones and the same chart builder, and then differ in ways worth knowing.

**Naming a cycle or a module replaces the project rather than narrowing it.** On the two totals routes the work items become whichever ones that cycle or module holds, and the project in the url stops mattering — as do the analytics filters on the work items themselves, which move onto the link table instead. The cycle is only checked against the workspace, so a cycle belonging to a different project of the same workspace is accepted and its work items are counted. The custom chart makes the opposite choice: there the cycle or module *narrows* a set that is already the project's.

**The per-assignee split includes a row for nobody.** The assignee join is an outer one, so work items with no assignee group together under an empty name and an empty id. The join also does not check whether the assignment was taken back, so a deleted assignee link still puts that person in the list.

**The completion chart is two different charts wearing one name.** For a project it is monthly, its count is what was created, and it runs to the current month whatever the caller asked for. For a cycle or a module it is **daily**, it counts *links* rather than work items — so the created curve is when work items were put into the cycle rather than when they were raised — and its count is the created and the completed added together rather than the created alone.

Three ways it reaches a 500, all of them upstream's: a cycle with a start date and no end date, a module with a start date and no target date, and a project id that does not exist. A cycle or module with **no** start date is different — that answers with an empty chart rather than failing.

## Migrated module: the advance analytics

The three workspace-level routes are implemented and cut over: the totals across the top of the page, the per-project split, and the three charts. The project-scoped copies of all three are a separate set of views and are not migrated yet.

**Two date shapes, and they do not overlap.** The totals route asks for a pair of timestamps compared against `created_at`; the chart routes ask for a pair of dates compared against `created_at`'s date. A route asks for one and gets nothing for the other, and a `date_filter` neither of them recognises is not an error — it simply leaves the numbers unnarrowed.

**Naming projects changes what "users" means.** Without `project_ids` the overview counts the people in the workspace; with them it counts the *memberships* of those projects, so somebody in two of the named projects is counted twice. The projects chart's member total is a third thing again: it counts every active workspace member including the bots, and ignores `project_ids` entirely.

**The per-project split ignores the dates.** It asks for the chart range and then calls the method that does not use it; the one that does is unreachable. So those rows are the whole history however the caller narrows the dates.

**The intake total reads through the plain manager** rather than `issue_objects`, so unlike every other number on the page it includes the archived work items, the drafts and the ones still in triage. The five statuses it accepts are every status there is, which makes it "work items that arrived through an intake" rather than anything narrower.

The **completion chart** draws one point per month from the workspace's first month to this one. Narrowing the dates moves the first month but not the last: the loop always runs to the current month, so a range ending last year still draws every month since as an empty one.

The **custom chart** counts *distinct* work items, so a work item with three labels adds one to each of three bars rather than three to any of them. The SQL for all thirteen axes was taken from the real ORM rather than written from the field names, which settled the question the field map raises — whether the soft-delete rule on a relation gets its own join or shares the one the key is read from. It shares it.

## Migrated module: the intake cutover and the intake's description versions

The work items inside an intake were implemented some time ago but the proxy never reached them: the matcher covered `intakes/` and `inboxes/` and stopped there, so all ten routes — five methods under each of the two names the viewset is mounted with — were still being answered by Django. They are cut over now, with no code change behind them.

The two **intake description-version** routes are added at the same time, because they sit on the same path prefix. The detail route is the work item's endpoint reached through a second url and nothing more. The list is not: the work item copy sorts newest first, this one applies no ordering at all, and the model declares none either — so what comes back is whatever order the database chose, and a second page can repeat or skip a version. Reproduced rather than corrected, since adding an order would change what the endpoint returns.

## Migrated module: one person's corner of a workspace

Six routes are implemented and cut over: the profile, the numbers, the activity feed and its csv export, the recent visits, and the per-project member map.

**The profile guards nothing but the session.** What stands in for a permission is the pair of lookups it opens with — the caller has to be an active member of the workspace and so does the person being asked about — and either one missing is a 404 rather than a 403. A guest gets the person but an empty project list, because the numbers are only built from member level up.

**Its four counts are counted over one joined row set rather than four.** The assignee join is added for three of them, and all four then count rows of that join rather than work items, so `created_issues` is larger than the number of work items somebody raised whenever any of them has more than one assignee. The joins are the plain ones Django writes for a related lookup, which do not apply the soft-delete manager either — a deleted work item still counts. The SQL was taken from the real ORM rather than written from the model, which is the only way this was visible at all.

**Any filter at all makes the stats route a 500.** The subscribed count applies the work item filters to `IssueSubscriber`, which has none of those fields, and Django raises `FieldError` before the response is built. So `?priority=high` on `user-stats/` is a 500 today, and it is one here. The two cycle lists in that same response are not made distinct, so a person with three work items in one cycle sees that cycle three times.

**Everything narrows to projects the caller belongs to**, not the person being asked about, so two people looking at the same profile can see different totals.

The **export** does not apply the archived-project rule that the json feed does, so it reads activity out of archived projects too. Every cell is quoted and a value opening with `=`, `+`, `-` or `@` is prefixed with a quote, which is what keeps a work item named like a formula from being run as one when the file is opened.

The **member map** picks its projects from the caller's membership anywhere rather than in this workspace, and only then narrows to the workspace the url names. Two workspaces cannot share a project so the extra breadth changes nothing, but it is why the query reads the way it does.

## Migrated module: draft work items

The five draft routes and the one that raises a draft are implemented and cut over. A draft is a work item somebody started and has not committed to yet, which is why it needs no project: the whole point is that the decision can wait.

Three things about it are not what you would guess, and all three are reproduced rather than corrected.

**Only a workspace admin can read a draft back.** The read allows the admin role alone, and the creator rule beside it names `Issue` rather than `DraftIssue` — so it looks the draft's id up in the work item table, where it will never be. The edit has the same slip, which is why a guest cannot edit a draft they made themselves. The delete is the one route whose creator rule names the right model.

**Most filters are a 500.** `issue_filters` emits lookups like `label_issue__deleted_at__isnull` alongside `labels__in`, and those reach through related names that belong to `Issue`. A `DraftIssue` has none of them, so Django raises `FieldError`. Every lookup that needs a join is refused here for exactly that reason; what is left — priority, state, parent, project, the two dates, the three timestamps, `created_by`, the name search and `state__group` — is translated onto the draft's own table.

**The project is taken on trust.** The view reads `project_id` straight out of the request body and hands it to the model, so a project from another workspace is accepted and the draft follows it: `WorkspaceBaseModel.save` reads the workspace off the project rather than from the url. The related rows it writes keep the workspace the url named, so a draft aimed across a workspace boundary ends up with its links in one workspace and itself in another.

`DraftIssue.save` is close to `Issue.save` but not the same. There is no sequence number and so no advisory lock. The sort order is recomputed on creation **even when the request asked for one**. And `completed_at` follows the state on every save rather than only when the state changes, so editing anything at all on a finished draft rewrites the moment it was finished.

Raising the draft runs the work item's own creation path over the **request body**, not over the draft — a field filled in on the draft and left out of this request is not carried over. The assets that were uploaded against the draft do move onto the work item. The cycle activity it sends names its project from a url keyword this route does not have, so Django sends the literal string `"None"` and the task then finds no project by that id; moving a draft into a cycle therefore records no cycle activity today.

## Migrated module: API keys

The five key routes are implemented and cut over. A key is a fixed prefix and thirty-two hexadecimal characters, which is what makes one recognisable in a log.

The **create and the update** report the key itself; the two reads do not. A key not written down when it was made cannot be recovered, and that asymmetry is the whole of the difference between the two serializers. An unnamed key is given a label of thirty-two hexadecimal characters — the same shape as the key's own suffix — and a bot's key is marked as one, which is what tells the external API it is not a person.

A **service** key is hidden from every route here: those belong to the installation rather than to a person, and nobody reaches one through this API.

## Migrated module: profile images

`POST` on `assets/v2/user-assets/` and `PATCH` and `DELETE` on `<uuid>/` are implemented and cut over, which completes the v2 asset API.

A profile image belongs to a **person** rather than to a workspace, so nothing here is scoped to one and the asset key has no workspace in front of it — the only asset key the application writes that does not. The entity has to be `USER_AVATAR` or `USER_COVER`, the type has to be one of five images, and a name that sanitizes away to nothing is stored as `unnamed` rather than refused.

The `PATCH` puts the image **on the person**: it replaces whatever was there, marks the one it replaces deleted, and clears the url the person used to carry, so an uploaded image always wins over a linked one. The `DELETE` takes it off again. Both drop the two cached views of the person.

## Migrated module: workspace assets

The workspace half of the v2 asset API is implemented and cut over: `POST` on `assets/v2/workspaces/<slug>/`, `GET`, `PATCH` and `DELETE` on `<uuid>/`, and the `check/`, `download/`, `restore/` and `static/` routes. The project half and `duplicate-assets/` are still Django's.

An upload names the **entity** it belongs to, and the entity decides which column its identifier is written to — a workspace logo writes `workspace_id`, a page description writes `page_id`, and the two draft entities are accepted and write no column at all, so their identifier is dropped. This endpoint holds every upload to an **image** however it names itself, so an `ISSUE_ATTACHMENT` reserved here cannot be a pdf while the same entity reserved through the project route can. A workspace logo is the one entity with a role of its own: only a workspace admin may reserve one, whatever the route's permission allows.

The `PATCH` does more than mark the bytes present: it **moves the asset onto its entity**. A workspace logo or a project cover replaces whatever was there, the one it replaces is marked deleted, and the url the entity used to carry is cleared — so an uploaded image always wins over a linked one. Only those two entities have that step; everything else is just marked uploaded.

`static/<uuid>/` is the route every avatar and logo url points at, and the one asset route with **no authentication at all**. It serves only the four entities a person or a workspace wears, which is what keeps a work item's attachment off an unauthenticated route, and a type a browser would execute is served as an attachment rather than inline. It signs without a filename, so the browser keeps the name the object has in the bucket.

Two smaller shapes: `check/` answers `200` with `false` rather than a `404` when the asset is not there, and `restore/` reads through the manager that shows deleted rows — the only route here that does. The three detail routes are authorised at the **workspace** level, so an asset bound to a project needs a membership of that project as well, or a workspace guest could reach a project they are not in.

## Migrated module: project assets

The project half of the v2 asset API is implemented and cut over, which completes it: the reserve under a project, the three detail routes, the project download, the bulk claim, and `duplicate-assets/`.

The two halves are **not the same endpoint with a project id added**. The project route asks nothing of the entity beyond being one — a workspace logo reserved here is written with the project's id beside it and nobody objects — and its `PATCH` and `DELETE` move the asset onto and off **nothing**, where the workspace route puts a logo or a cover on its owner. Its entity table differs by a single line: a draft issue description writes a column here and writes none there.

The bulk claim is what attaches an asset uploaded before its entity existed — a project cover uploaded during project creation has no project until this call gives it one. The entity it acts on is read off the **first** asset the query finds and then applied to all of them, so a call naming two assets of different kinds treats both as whatever the first one is. Its scope is the caller's own uploads that are either unattached or already in this project, and an entity that has been deleted since the upload is a foreign key failure that three of the five kinds swallow.

`duplicate-assets/` copies the bytes inside the bucket rather than through the API, keeps the **unsanitized** name in the copy's attributes even though the key it is written under is sanitized, and marks the copy uploaded in a second statement after the row is written.

## Migrated module: stickies

The five sticky routes are implemented and cut over. A sticky is a note somebody keeps in a workspace and nobody else can see, so every route here is scoped to its owner rather than to a role — the update and the delete carry **no role at all**, only the rule that the note is the caller's.

The list pages **twenty** at a time rather than the paginator's usual thousand, and `query` searches the **stripped** text rather than the html, so a word that only appears inside a tag is not found.

A new note is placed after every note the **workspace** already holds — not after the caller's own — so two people's notes share one sequence and a new note lands behind a colleague's. The stripped copy follows the html on every save, and empty html leaves **no copy at all** rather than an empty one.

## Migrated module: favourites

The eight favourite routes are implemented and cut over: the workspace list, create, update, delete and folder contents, and the three older project-favourite routes that write into the same table.

A favourite that belongs to a project is reported only while the caller is still **in** that project. The top-level list additionally hides a **page** with no project — the one entity type it refuses — while the folder contents hide nothing, so a page inside a folder is reported where the same page outside one is not.

The serializer reads the thing itself out of whichever table its type names, and three types report a null beside them: a folder has no model, a type nobody recognises has none either, and a **work item** is in the table of types with no serializer next to it — so starring one shows nothing about it. A favourite whose thing has been deleted reports a null rather than failing the list.

Starring something twice answers the star that is already there rather than refusing it, and unstarring removes the row **for good** rather than soft deleting it, which is what lets the same thing be starred again.

The project favourite list is broken upstream and reproduced as such: the viewset inherits DRF's list and declares no `serializer_class`, so it asserts and answers `500`. Its create answers `204` with no body and writes the project into both identifier columns, reading the workspace off the project the way the model does.

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

## Migrated module: project estimates

The nine estimate routes are implemented and cut over: the project's scale (`project-estimates/`), the scale list, create, retrieve, update and delete, and the three that act on a single point.

Four behaviours are reproduced rather than tidied:

- A create whose payload has no `estimate` object at all is a **500**, because Django reads the name off it without checking that it is there. A payload that has one but no name gets a **random ten-letter name** — the one place in this API that names something for the caller.
- The update refuses a payload with no `estimate_points` even when it only means to rename the scale.
- A point's create asks that both the key and the value be **truthy**, so a key of zero is refused even though zero is where a scale starts.
- A point's delete answers with the points whose **key moved** to close the gap, not with the one that was deleted. The work items that used it are moved to whichever point `new_estimate_id` names, or left pointing at nothing when it names none, and either way each of them gets an activity of its own — one per work item, with no notification and no origin.

The scale and the points written with it are authored differently: a scale records nobody, while the points the create writes record the caller. `project-estimates/` answers an **empty list** when the project uses no scale — not a null and not a 404.

## Migrated space module: the work item list

`GET` on `issues/` under an anchor is implemented and cut over, which **completes the space app**.

The work is the session API's rather than this one's: the filters, the ordering, the annotations and both grouped paginators are the same code, reached through one exported entry point, because the board shows the same list to somebody who is not signed in. What differs is the way in — an anchor rather than a membership — and that the project comes off the board rather than out of the url, read from its **entity identifier** rather than its project column.

The parts that need an account are the parts that are left out: no project lookup of its own, no guest rule, no recorded visit.

## Migrated space module: the work item detail

`GET` on `issues/<uuid>/` under an anchor is implemented and cut over. The **list** beside it is the one route of this app still on Django: it needs the grouped paginator, the same machinery the session API's own work item list is waiting on.

A work item that is not there is answered with a **null body and a 200** rather than a `404`, because the view serializes whatever the query returned and the query returned nothing. The work item is read through the manager the board itself reads, so a draft, an archived one or one in triage is not here whatever its id.

Its votes and its reactions are folded into the body, each with the person who left it. The reaction's `avatar_url` is computed from the **voter's** avatar rather than the reactor's — the expression names the votes relation where it means the reactions one — so a work item with reactions and no votes reports a null url for every reaction, and one with both reports whichever voter the join lands on. Reproduced rather than corrected: the `avatar` beside it is the reactor's, and a client that reads either one sees what Django shows it.

## Migrated space module: assets

The six asset routes a published board carries are implemented and cut over. The read is the only one **without a session**: a board's images are public and everything that writes one is not.

The read serves two entity types and nothing else — a work item's description and a comment's — so whatever else the board holds is out of reach here. A type a browser would execute is served as an **attachment** rather than inline, which is what stops an uploaded SVG running as script on the board's own origin.

An upload may name **any** entity there is, and the identifier it carries is written to `comment_id` and to nothing else — so an upload calling itself a work item description still lands on a comment, or on no comment at all. The size is clamped at **both** ends here, unlike every other reserve in the codebase, so an upload of nothing still carries a policy the bucket accepts.

The bulk claim moves assets onto a comment and **only** onto a comment: the entity is read off the first asset the query finds, and anything that is not a comment description is left where it is. The delete asks that the board be a project's and the update does not, which is the one difference between the two lookups.

## Migrated space module: the intake queue

The seven intake routes are implemented and cut over. The queue is mounted **twice** under two names — `intake-issues/` and the older `inbox-issues/` — and the older one serves only the list and the create.

Filtering the queue reads the same parameters every other list does, so the filter machinery moved out of the session package into `internal/issuefilters`, which is now where `issue_filters` and the SQL it becomes live for all three applications.

Three behaviours are reproduced rather than tidied:

- The create writes the work item **directly** rather than through the serializer, so it takes no sequence number, no sort order and no default assignee — the same shortcut the external API's intake create takes.
- The priority that is **checked** and the priority that is **written** are not the same: an absent one passes the check as `none` and is then written as `low`.
- Editing or removing somebody else's entry is a **400** rather than a `403`, and an edit takes only three fields — the name, the html and the json — whatever else the payload carries.

The list reads through the plain manager rather than the work item one, so a draft or an archived work item in the queue is reported, ordered by when each is snoozed until and then by its status. A board with no intake refuses every one of these routes with the same `400`, and the create additionally refuses an intake that is not the board's own.

## Migrated space module: comments

The five comment routes are implemented and cut over. This is the one module in the app where the line between reading and writing runs through a single viewset: the list and the retrieve carry **no session** and the create, the update and the delete carry one.

A published board only ever shows **external** comments, and every route here holds to that: the reads filter to them, the create writes one whatever the payload's access says, and the update and the delete find a comment only if the caller wrote it themselves.

A board with comments switched off answers the list with an **empty list** rather than a refusal, while the three writes answer a `400` naming the switch. The serializer's `is_member` is annotated for the **caller** rather than for the comment's author, so it is false on every comment the two unauthenticated reads report — whoever wrote them.

The comment's stripped copy follows its html on every save, and a comment with no html has an **empty** stripped copy rather than a null, which is where it differs from a work item's description.

## Migrated space module: votes and reactions

The nine routes a reader writes with are implemented and cut over: the votes on a work item, the reactions on one, and the reactions on a comment.

Reading a published board needs no account; **writing on one does**. Each of these carries a session, and each binds what it writes to the board first — a work item is checked through the same manager the board reads, so nothing the board hides can be voted on, and a comment has to be an **external** one, so nothing internal can be reacted to.

Two of the three lists are **always empty**, and both are reproduced rather than corrected. The vote list looks its board up by **workspace slug** and is handed the anchor, so the lookup never matches. The work item reaction list reads two url parameters its route does not carry, so its lookup fails the same way. A list that starts returning rows is a change no client asked for, and the board reads both off the work item itself.

Writing remembers the reader. Somebody who is not in the project is recorded as having been on its board, which is how a published project counts the people reading it. The vote defaults to **up** when the payload names none, and withdrawing a vote asks nothing about whether votes are still enabled — so one cast before the board was closed can still be taken back.

## Migrated space module: the project surface

`settings/`, `meta/`, `members/`, `states/`, `labels/`, `cycles/` and `modules/` under an anchor, and the `anchor/` lookup from the other end, are implemented and cut over.

The shapes are narrow on purpose — a published board shows what a reader needs and nothing else. `cycles/` and `modules/` report an id and a name and nothing more, **archived ones included**, because the published board has no notion of an archive. `labels/` names a parent rather than nesting it. `states/` hides the triage state **by name** rather than by its flag, so a state somebody renamed is reported and one they called Triage is not, whatever it actually is.

`members/` reports the avatar **column** rather than the url every other API reports, so a member whose picture is an uploaded asset comes back with an empty avatar here. That is upstream's and is left as it is.

## Migrated module: the project sidebar, archive and identifiers

Five more project routes are implemented and cut over: `projects/details/`, the archive and unarchive, and the two identifier routes.

`projects/details/` is the project list ordered for a sidebar — by the caller's own place in it and then by name, with a project they have no ordering for sorting last. It paginates only when **both** `per_page` and `cursor` are given and answers a plain list otherwise, so a caller who sends one of the two gets every project rather than a page. Both list routes read the same annotated set and apply the same guest and member narrowing.

Archiving a project **removes every favourite pointing at it**, for everybody rather than for the caller alone, and unarchiving does not bring them back. The identifier check upper-cases and trims the name before it looks, so it agrees with what a create would write, and reports a **count** beside the rows. Freeing an identifier is refused while a project still carries it, and the row goes for good rather than being soft deleted — which is what lets the name be taken again.

## Migrated module: project deploy boards

The five deploy board routes are implemented and cut over. A deploy board is what makes a project readable without an account: the anchor in its url — thirty-two hexadecimal characters — is the whole of the credential.

Two shapes are worth naming. The list route reports **one board** rather than a list, and a project that was never published is answered with the serializer over **nothing**: twelve keys of defaults, no id and no anchor, rather than an empty list or a `404`. A serializer with no instance reports the fields that could be written and the defaults they would take, so every read-only field is absent rather than null.

Publishing answers `200` whether it made the board or edited one, and writes every switch the payload does not name as **off** — so a second call that means to turn comments on turns votes and reactions off with it.

One deliberate divergence: the retrieve, the update and the delete are scoped to the project in the url. Django looks the board up by id alone, because the viewset's queryset is every board there is, so a board of another project could be read or edited through a project the caller happens to be in.

## Migrated module: project invitations

The eight invitation routes are implemented and cut over: the project's invitation list, create, retrieve and delete, the two public join routes, and the caller's own invitations and the call that joins projects with them.

**The create has never sent an invitation.** It reads `.role` off a **queryset** rather than off a row, which is an attribute error, so every call that names an email ends in a `500`. The line after it is broken the same way — the list of invitations it has just built shadows the task it means to call — but nothing reaches that far. A call naming no emails is refused before either, and that refusal is the only answer this route gives that is not a `500`. The port answers exactly that.

The two join routes carry **no session**: the token in the payload stands in for one. The token is checked first and the session second, so a caller with the right token and no session is told to sign in rather than that the token is wrong; the signed-in person then has to be the one the invitation names. The public view reports only what an invitee needs to decide — the project, the workspace, the role, whether it has been answered — and never the token or the email.

Accepting puts the invitee into the workspace and then into the project, and two details there are upstream's. A workspace membership made this way is capped at **member** however high the project role is, so an invitation to administer a project does not hand out the workspace. And the project membership is looked up by workspace and member rather than by project, so somebody already in *another* project of the same workspace is reactivated there rather than added to this one.

`users/me/workspaces/<slug>/projects/invitations/` is not scoped to the workspace in its own url, so it reports every project invitation the caller has anywhere. Joining narrows the ids to the workspace **before** the secret-project check runs, so an id from another workspace is dropped rather than refused.

One deliberate divergence: the list, the retrieve and the delete ask for an active membership of the project. Django asks only that the caller be signed in — the viewset carries no permission class and its queryset is scoped to the project alone, so any account could read any project's invitations, emails included. The workspace's own invitation list already asks for admin, and every other project-scoped read here asks for membership.

## Migrated module: the workspace-wide lists

`labels/`, `states/`, `cycles/` and `modules/` under a workspace are implemented and cut over. They are the same question asked four ways — everything of a kind the caller can see across a workspace — and each is scoped to the projects they are an **active member** of, skipping an archived project, so the answer is what their sidebar could show rather than what the workspace holds.

Two shapes differ from the project's own list of the same thing. A state's `order` is its place as a fraction of its group, and the group is counted **across the whole workspace** here, so one state is ordered differently in the two lists. The cycle list reports neither the assignees nor the version its project's list carries, and renders its dates in the **caller's** timezone where the project's list uses the project's — the same asymmetry, the other way round. The module list carries the archive stamp its project's list leaves out, and comes back newest first rather than favourites first.

## Migrated module: project states

`GET` and `POST` on `states/`, `GET`, `PATCH` and `DELETE` on `states/<uuid>/`, `POST` on `mark-default/`, and `GET` on `intake-state/` are implemented and cut over.

The list is the one shape that carries `order`, which is not a column. States are numbered within their own **group** and each reports its place as a fraction of that group's size, so a group of four reads 0.25, 0.5, 0.75, 1. The serializer declares the field and an instance never has it, so every other route drops the key rather than returning a null. `grouped=true` answers an object keyed by group rather than a list, with the groups in the order their names sort.

The create answers **200** rather than 201, and a name that is taken is a `400` rather than a conflict — on both the create and the update, since both read the database's own wording. The update is open to every member **including a guest**, which is the only write on a project's workflow that is; the create, the delete and the default switch are admin-only.

Triage is refused by the serializer's `validate`, so it lands under `non_field_errors` rather than under the field, and the triage state is hidden from every route here but `intake-state/`. The delete refuses two states: the project's default, and any state that still holds work — and the emptiness check reads the plain manager, so an archived or draft work item keeps its state alive while a soft-deleted one does not.

Three of the routes drop the cached workspace state list and the update does not, which is upstream's and is left as it is. `mark-default` runs two unguarded updates: naming a state that is not there clears the project's default and sets nothing.

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

`internal/project/issue_save_path.go` is `Issue.save`'s non-adding path, which runs whenever a serializer saves a work item and writes three columns the request never mentions. The state is filled in when it is missing, so `state_id: null` does not clear a work item's state — it moves it onto the project's default. `completed_at` follows the state whenever the state really changes: into a completed group it becomes the moment of the change, out of one it becomes null, and a save that names the state a work item already had leaves it alone rather than rewriting the moment the work item was finished. And `description_stripped` is recomputed from whatever html the work item ends up with, so the plain-text copy cannot fall behind the rich text. The two audit columns are written whatever else changed, because the serializer sets `updated_at` by hand before saving — an update that moves only the assignees still touches the work item's own row. The intake update runs through the same serializer and so the same path.

## Migrated module: sub-issues

`GET` and `POST` on `issues/<uuid>/sub-issues/` are implemented. Both shapes come from the SQL Django actually renders rather than from reading the view, because several of its filters are not where they look like they are.

`Issue.issue_objects` hides more than soft-deleted rows: it also excludes triage issues, archived issues, draft issues, and issues whose project is archived. An issue with no state passes the triage check, since Django's `exclude()` over a nullable join renders as `NOT (group = 'triage' AND group IS NOT NULL)`. `internal/project.issueObjectsPredicate` is that whole manager, and `sub_issues_count` applies it too.

The annotation joins deliberately keep two gaps. Neither the `project_members` join behind `assignee_ids` nor the `modules` join behind `module_ids` filters `deleted_at`, because a model's default manager only filters that model's own queryset, never a join traversed through it. A soft-deleted project membership therefore still keeps its assignee in `assignee_ids`, exactly as on Django.

The two responses are different shapes. The read route returns `values()`, which is `IssueSerializer`'s twenty-five fields plus `state_group`, with `created_at` and `updated_at` moved into the caller's timezone and nothing else converted. The assign route serializes plain model instances through the same serializer, and none of the seven annotated fields exist on an instance; DRF makes a read-only field not required, so `get_attribute` raises `SkipField` for each and the key is dropped rather than rendered as null, leaving eighteen.

`group_by=assignees__ids` fans an issue out across each of its assignees and files an unassigned one under the literal string `None`. Any other `group_by` is looked up straight in the `values()` dict, so a name that is not one of its keys raises `KeyError`, which `BaseAPIView.handle_exception` turns into a `400` rather than a `500`.

`internal/project/issue_ordering.go` is `order_issue_queryset`. Its fixture, `testdata/issue_order_by.tsv`, is the `ORDER BY` Django renders for each of the fourteen allowlisted fields in both directions, extracted from Django rather than written by hand — which is what caught three divergences that reading the Python did not. No clause pins a `NULLS` position, because Django emits a bare `ASC`/`DESC` and leaves Postgres to apply its defaults. The priority `Case` has no default, so a priority outside the list sorts as null. And Django orders by that `Case` ascending in *both* directions — only the string handed back to the paginator flips — so `priority` and `-priority` return the same order, which is reproduced rather than corrected. State group is the one that really does reverse, by reversing the list the `Case` is built from.

The assign route scopes both the parent lookup and the sub-issue ids to the URL workspace and project, and fires `issue_activity` only for the ids that were really re-parented, so a foreign id cannot reach the task and bump `updated_at` on an issue the caller cannot see.

## Migrated module: the api key activity log

Every request made with an api key is written down, which is what an installation's administrator reads to see what a key has been doing. Django does it in two halves — `APITokenLogMiddleware` builds the record and hands it to Celery, and `plane.bgtasks.logger_task.process_logs` writes the row — and both halves are here: `internal/server/api_activity_log.go` and `internal/worker/api_logs.go`.

The Go side had neither until now, so the table had a hole in it wherever the proxy had cut a path over: a request the Go server answered was served correctly and never recorded. Closing that is most of the point of this piece, and it is why the middleware is registered globally rather than only on `/api/v1/`, matching Django's `MIDDLEWARE` list.

The key itself is never written down. What goes into `token_identifier` is `hmac-sha256(SECRET_KEY, key)` in hex — keyed rather than a bare digest, so somebody holding a key cannot work out which rows are theirs by hashing it. The same three headers Django redacts (`x-api-key`, `authorization`, `cookie`) are replaced with `[REDACTED]` rather than dropped, so a reader can see that a key was sent without seeing which.

`headers` is a string, not json: the column holds what `str()` made of a python dict, so `renderRequestHeaders` reproduces `repr` — including its quote rule, where a value containing an apostrophe is wrapped in double quotes instead of being escaped. Django writes the headers in the order the server handed them over; a Go `http.Header` is a map with no order at all, so they are written in name order instead. The set and the values are the same either way.

Bodies follow `_safe_decode_body` exactly, quirks included. An empty body is null rather than an empty string. A body starting with the PNG, JPEG or PDF signature is recorded as `[Binary Content]` without being decoded, even though a PDF's first bytes decode fine. Anything else that is not valid UTF-8 becomes `[Could not decode content]`. A body carrying a null byte decodes and is kept, and it is the worker's insert that then fails on it, since Postgres will not take a null byte in a text column — the record is lost, which is what happens upstream and is reproduced rather than corrected.

`ip_address` is `get_client_ip`, which takes the first address `X-Forwarded-For` names without trimming it: a header written with a space after each comma leaves the space on the *second* address, not the first, so the value stored is clean by accident rather than by design. With no such header it is the address that connected.

The task takes whatever keywords it is given and ignores the ones it does not know, which is what the upstream `**_` is for — a record queued by an older release, which passed a `mongo_log` argument, still writes its row rather than failing. Neither half fails the request: a record that cannot be queued is dropped after the response has already gone out, and a row that cannot be written is logged and abandoned, which is `log_to_postgres` returning `False` to a caller that never looks.

## Migrated module: work item exports

`internal/project/exporter_handler.go` records that an export was asked for; `internal/worker/export.go` builds it. The two routes are `GET` and `POST` on `/api/workspaces/<slug>/export-issues/`, and the task is `plane.bgtasks.export_task.issue_export_task`.

The route takes the project ids on trust. A caller who names them is not checked against them, and only a caller who names none gets the list of projects they are really a member of. What keeps somebody else's work out of the file is the worker's own queryset, which joins the membership table again — so naming a foreign project produces an export with nothing in it rather than one with another team's work items.

The export queryset uses `Issue.objects`, not `Issue.issue_objects`. Drafts, archived work items and everything sitting in a project's intake are therefore all exported alongside the rest, which is not what any other work item list on the API does. And the membership join filters `is_active` without filtering `deleted_at`, so somebody who left a project and rejoined it has two rows there and every one of that project's work items comes back twice. Both are reproduced rather than corrected.

The serializer declares twenty-eight fields and renders twenty-five. `sub_issues_count`, `link_count` and `attachment_count` are annotations the export queryset never adds, and DRF drops a read-only field whose attribute is missing rather than rendering it as null — the same rule that shortens the other serializers on instances. So the spreadsheet has twenty-five columns.

The three formatters are ported in `internal/worker/export_format.go` and checked against the real ones. `testdata/export_format.json` is what `CSVFormatter`, `JSONFormatter` and `XLSXFormatter` produce for three rows chosen to break an encoder — formula triggers, embedded quotes and newlines, non-ASCII text, an apostrophe inside a nested object — and CI regenerates it. The csv and the json are compared byte for byte; the spreadsheet is compared cell for cell, since two writers never produce the same zip.

That corpus is what caught the parts nobody would have got right by reading. Go's own csv writer rewrites a newline *inside* a field as CRLF along with the one between records, and a work item whose name has a line break in it is not unusual, so the writer here is hand-rolled — python leaves the one in the field alone. Python's `json.dumps` escapes every non-ASCII character as `\uXXXX` and leaves `<`, `>` and `&` alone, where Go's encoder does the opposite on both counts.

The same list reaches the two formats differently, which is a difference between the formatters rather than a mistake in either. The csv flattens and writes a list as json, so the links column reads `[{"url": "...", "title": "..."}]`. The spreadsheet does not flatten and joins a list with `", "` through `str()`, so the same column reads `{'url': '...', 'title': "..."}` — a python dict repr, apostrophe quoting rule included. Every cell in both, header row included, goes through `sanitize_csv_value` first, which prefixes a value starting with `=`, `+`, `-`, `@`, a tab, a carriage return or a newline with an apostrophe so a spreadsheet reads it as text.

The key an export is stored under carries only the first six characters of its token, so two exports of the same workspace made on the same day overwrite each other unless their tokens differ in those six characters. The link is presigned for seven days. On MinIO it is signed against a second client built from `WEB_URL`, because the browser reaches the bucket through the web host and not through the internal one — Django builds that endpoint by stripping the literal string `/uploads` from its custom domain, which only removes the bucket name when the bucket is actually called `uploads`; a differently named bucket leaves it in the endpoint's path and produces a link that does not work. That last part is not reproduced, since it is a broken configuration either way.

Failure is reported onto the record and nowhere else: anything that goes wrong sets `status` to `failed` and writes the exception's message into `reason`, which is what the web app shows beside a failed export. `save(update_fields=...)` names only the columns it changes, so `updated_at` is left where it was even though it is an `auto_now` column. The one failure that cannot be reported is a token naming no record at all — Django reads it again inside its own `except` block, so that raises a second time and the task dies without telling anyone.

## Migrated service: the analytics export

`analytic_export_task` emails somebody the spreadsheet of the chart they were looking at. The rows are built in `internal/project/analytics_export.go`, beside the analytics endpoint rather than in the worker, because it is the same chart: `build_graph_plot`, the filter translation and the lookup tables are all there already, and a second copy in the worker would be a second thing to keep in step. The worker's half — `internal/worker/analytic_export.go` — turns those rows into a csv and sends it.

Nothing that goes wrong is reported anywhere. The whole task body sits in one `try`, and its `except` logs and returns, so a bad axis, a filter the ORM refuses, and a mail server that will not answer all end the same way: the person who asked waits for an email that never arrives. That is reproduced rather than corrected, which is why the errors on this path are logged and swallowed rather than returned.

The exported grid carries several things worth knowing before reading one:

- A chart grouped by state is headed literally `X-Axis`. `row_mapping` names `state__name`, which is not a valid axis, and does not name `state_id`, which is.
- An id keeps its raw uuid whenever the lookup table does not name it. For assignees that is routine rather than rare: `get_assignee_details` only lists people who have a picture, so anyone who never set an avatar appears in the file as a uuid.
- The assignee name is written by an f-string over the first and last name rather than by the model's own `full_name`, so it is not trimmed — somebody with no last name is followed by a trailing space.
- A chart segmented by module never renames its segment columns, because `generate_segmented_rows` looks module segments up in `label_details`. With no label axis that table is empty and the lookup quietly finds nothing. With a label axis it is full of label rows, and asking one for its module id raises `KeyError` — so a chart grouped by label and segmented by module produces no file and no email at all. Both are reproduced.
- The label lookup is the one that reads through `Issue.objects` rather than `Issue.issue_objects`, so a label is listed even when every work item carrying it is archived or a draft.
- A bucket with nothing in a given segment is written as the string `"0"`, not as a number.

The segment columns are the one place this deliberately differs. Upstream builds their headings out of a python `set`, so their order is whatever that set happens to iterate in — which changes between runs, since python randomises string hashing. They are sorted here. The columns are the same columns; only their order is decided rather than left to chance.

`generate_csv_from_rows` asks for `QUOTE_ALL`, where the work item export's formatter takes the default `QUOTE_MINIMAL`, so the same value is written two different ways depending on which export produced it. Both run every cell through `sanitize_csv_value` first, and both sanitise *before* rendering — which is why a negative count keeps its minus sign while a string that merely starts with one is prefixed with an apostrophe.

The email itself has no html part. Django renders `emails/exports/analytics.html` only to turn it into plain text and never attaches it, so what arrives is a text body with a spreadsheet beside it. The template is a copy of the one `apps/api` ships and CI diffs the two, the same way it does for the notification email.

`export_analytics_to_csv_email` is ported alongside it. Nothing in this edition queues it; it is handled so that a message carrying its name does not sit unconsumed.

## Shared: BeautifulSoup's reading of html

`internal/soup` reads and writes html the way `BeautifulSoup(content, "html.parser")` does. It exists because the asset copier stores what bs4 made of a description, so whatever it stores has to be bs4's reading of it and not somebody else's.

It is deliberately a second html round trip rather than a reuse of `internal/htmlsanitizer`. That one is libxml2's, reached through lxml, and it corrects markup the way a browser would: a paragraph is closed before a block element opens, a table's rows are moved inside the table. Python's own `html.parser` corrects almost nothing — it keeps a stack, pushes on a start tag and pops to the most recent matching name on an end tag. So `<p>a<div>b</div></p>` keeps the div inside the paragraph, `<p>a<p>b</p>` nests one paragraph inside the other, `<ul><li>a<li>b</ul>` nests the second item inside the first, and `<b>x<i>y</b>z</i>` closes both on the `</b>` and then drops the `</i>`. Running a description through the wrong one of these would quietly rewrite the document.

The tokens come from `x/net/html`'s tokenizer, which agrees with python's on everything a description carries; the tree above them is bs4's, which is where the two really part company.

`testdata/round_trip.json` is what bs4 produces for fifty descriptions — what the editor really writes, followed by the markup that breaks a serialiser — and CI regenerates it. It is what pinned down the parts nobody would have guessed:

- **Attributes come out in name order**, not the order they were written. bs4's formatter sorts them, so `<img src="x" alt="a">` is written back as `<img alt="a" src="x"/>`.
- A `class` written with two spaces in it comes back with one, because bs4 splits the handful of attributes in `DEFAULT_CDATA_LIST_ATTRIBUTES` on whitespace and joins them with a single space.
- Void elements are bs4's list, not the html5 one, so `image`, `isindex`, `nextid` and `spacer` self-close along with `br` and `img`.
- A custom tag written `<x/>` is written back as `<x></x>`; only the tags on that list ever self-close.
- Only `&`, `<` and `>` are escaped in text; a quote is left alone. An attribute carrying a double quote and no single one is wrapped in single quotes rather than escaped.
- `script` and `style` contents are written back exactly as they were read.

## Migrated service: copying a description's pictures

`copy_s3_objects_of_description_and_assets` runs when a page or a work item is duplicated, so the copy points at its own files rather than at the original's. `internal/worker/copy_assets.go` is the port.

Four steps: read the asset ids out of the description, duplicate each asset in the bucket and in the table, rewrite the description to point at the copies, and ask the live server to turn the new html into the two representations the editor reads. The whole thing sits in one `try` upstream whose `except` logs and returns, so a copy whose pictures could not be duplicated simply keeps pointing at the original's — reproduced rather than corrected, which is why every failure here is logged and swallowed.

**The user who asked for the copy is thrown away.** `user_id` is handed to `FileAsset.objects.create(created_by_id=...)` and then discarded, because `BaseModel.save` reads the current user out of crum rather than off the instance and a worker has no current user. The same blanking hits the page or work item itself: duplicating a page forgets who made it. Both are reproduced, and the second is the same rule already documented for the link crawler.

The originals are looked up by workspace and project as well as by id, so a description naming a picture from another project copies nothing for it and keeps pointing at the original. Only three of an asset's attributes are carried over — name, type and size — and anything else the original held is dropped. The copies are marked uploaded in one statement after the fact, which is what keeps a half-finished copy out of the sweep that deletes unuploaded assets.

`get_entity_id_field` does not name every entity type. `DRAFT_ISSUE_ATTACHMENT` is missing from it, so a copy of one is owned by nothing at all — none of the owner columns is written.

The live server is the one piece of this that reaches outside the installation, and it is still the Node service. With no `LIVE_BASE_URL` configured the call is skipped and the copy keeps whatever json and binary it already had, which is what upstream does too — the copy then opens with the original's content until somebody edits it. Any status other than 200 is treated the same way, since `requests` only raises when the call itself fails.

## Migrated service: the version backfill

`internal/worker/version_sync.go` is `plane.bgtasks.issue_version_sync` and `plane.bgtasks.issue_description_version_sync` — five task names between them. Nothing in the running application queues any of them. They are what the two management commands start, and they exist here so that an installation that runs those commands still gets its backfill once the queue has moved to Go.

Each backfill reads the work items in created order, writes one version row per work item, and asks for the next slice after a countdown. The whole batch is one transaction and the task swallows its own failure, so a backfill that breaks halfway stops there rather than skipping past it. A work item with no workspace or project is left out, and so is one with nobody to own its version — the owner is whoever last touched it, then whoever made it, and failing both any admin of its project.

**`IssueVersion.log_issue_version` always fails, and that is not a translation error.** It builds the row with `cls.objects.create(issue=issue, ...)` and never gives it a project. `ProjectBaseModel.save` then reads `self.project.workspace` off it, and an unsaved row whose `project_id` is None raises `RelatedObjectDoesNotExist` before anything reaches the database — which `log_issue_version`'s own `except` swallows and answers `False` to a caller that does not look. This was confirmed against the real model rather than read off the code.

That matters for `issue_task`, the third task in the module. It keeps one version row per person per ten minutes: an edit that lands within ten minutes of that person's last version is folded into it, and anything else calls `log_issue_version`. So the first edit after a gap records nothing at all, and only the folding branch has ever written anything. Reproduced rather than corrected. Nothing in this edition queues `issue_task` either.

The bulk writes differ between the two backfills for no reason anyone left behind: the work item one asks for a thousand rows at a time and the description one asks for all of them. Both are kept.

### Holding a task until its eta

The backfill is the first thing here to queue another task with a delay, so `Consumer` now honours the `eta` header and `CeleryPublisher.PublishAfter` writes it. Celery holds an eta task in the worker rather than at the broker and acknowledges it on receipt, so a restart loses it; this does the same. The wait happens out of the way of the messages behind it, and a shutdown waits for anything already running through `Consumer.Wait`.

The two `manage.py` commands that start these backfills are in `cmd/manage`.

## Migrated service: the management commands

`cmd/manage` is the `manage.py` commands an operator runs by hand — one binary with a subcommand per command, named exactly as `manage.py` names them. A runbook that says `python manage.py activate_user ada@example.test` becomes `pace-manage activate_user ada@example.test` and nothing else changes: the same arguments, the same wording on stdout, and the same non-zero exit when something was wrong. `internal/manage` holds the commands themselves.

All nineteen are here — the seventeen under `plane/db` and the two under `plane/license`.

Two of them gate a deploy, and they are the reason this piece exists at all — the worker and the beat wait on them before they start.

`wait_for_migrations` is the one that could not be a straight translation. Django asks its own migration executor whether anything is pending; there is no executor here, so the question is asked of the database instead: is every migration the Django app ships recorded in `django_migrations`? `internal/manage/migrations.tsv` is that list, generated from `apps/api` and regenerated by CI — a migration added there and not here would let a deploy through early, so the check fails the build rather than warning.

`wait_for_db` is the one place here that deliberately does something upstream does not. Upstream's loop asks for `connections["default"]`, which hands back a lazy wrapper without touching the server, so the loop never spins and the command reports success against a database that is not there. This one really connects. A command whose whole job is to wait is not worth reproducing as a command that does not.

Everything else follows the original, quirks included:

- `create_project_member`'s `--role` defaults to twenty, which is admin — so running it without the flag makes an administrator of the project rather than a member. Adding a new member also seeds that person's ordering for the project ten thousand below everything else they have, which is what `ProjectMember.save` does; updating an existing one does not, because that path never runs.
- `update_deleted_workspace_slug` reports `from 'X' to 'Y'` with the same slug on both sides, because it reads the old one off an object it has already changed.
- `reset_password` refuses a mismatch, a blank and a weak password in three different ways: the first two are written to stderr with a zero exit, and only the zxcvbn refusal is a `CommandError`.
- `update_bucket` only proves it can read an object when the bucket already holds one, so an empty bucket never clears that check and the command falls through to writing `permissions.json`.
- `clear_cache` flushes the whole Redis database rather than scanning for a prefix, which is what `django-redis`'s `clear()` does, and a single key is deleted under the `":1:"` prefix `django-redis` writes it with.
- Both bucket commands report every failure and exit zero, which is what lets them sit in a startup script ahead of the server.
- `fix_duplicate_sequences` asks for the workspace slug on the terminal rather than taking it as an argument, and takes the project's advisory lock before handing out numbers.

The email template `test_email` renders is a copy of the one `apps/api` ships, and CI diffs the two.

## Migrated service: the dummy project

`plane.bgtasks.dummy_data_task.create_dummy_data` fills a project with made-up work so somebody can see what a busy workspace looks like. `internal/worker/dummy_data.go` is the port, and `manage create_dummy_data` is what queues it.

What it writes is random by design, so this does not reproduce Faker's output: a name here is not the name Faker would have picked. What it does reproduce is the shape — how many rows of what, which columns are filled, which are left alone, and every place the original writes less than it looks like it does. `internal/worker/dummy_faker.go` holds the made-up values, kept to the same kinds and sizes: a person's name, a colour's name, a hex colour, a paragraph cut to a length, a date inside this year.

Four of those "writes less than it looks like" places are worth naming, because a reader of the Python would not spot them:

- **`create_issue_parent` writes nothing at all.** It builds an empty list, sets a parent on each sub-issue inside the loop, never appends any of them to that list, and hands the empty list to `bulk_update`. Not one parent is ever saved. Reproduced rather than corrected: a dummy project with a quarter of its work items suddenly parented would not be the project this task has always made.
- **The cycle count is one more than you ask for**, because the loop runs while the count is less than *or equal to* what was asked.
- **Intake work items are extra work items.** `create_intake_issues` calls `create_issues` again rather than filing any of the ones already made, so asking for a hundred and ten intake ones leaves a hundred and ten.
- **Labels and modules go on every work item, not half.** The line that picked half is commented out in both, while the assignees and the cycle links still pick half.

Three more things the shape carries:

- The five states it writes are not the six a real project starts with. The colours differ and there is no triage state at all, so a project made this way has nothing for its intake to file into.
- `bulk_create` skips the models' own `save`, so a state's slug and a page's stripped description are both left empty even though the html beside them is filled in.
- The sort order the work items start from is read off a *randomly chosen* state rather than off each work item's own, so the ordering it hands out means nothing.

`manage create_dummy_data` asks its questions in the order the command asks them, writes the workspace before the first project is asked about — so an answer given up halfway leaves a workspace behind — and splits the member emails on commas without trimming, so a space after a comma stays part of the address and matches nobody. All reproduced.

## Migrated service: seeding a new workspace

`plane.bgtasks.workspace_seed_task.workspace_seed` runs on every workspace anybody makes, and fills it with the demo project. `internal/worker/workspace_seed.go` is the port; the eight seed files are copied into `internal/worker/seeds/` and CI diffs them against `plane/seeds/data`, because a seed changed upstream and not here would seed a different workspace.

Everything it writes is created by a bot account it makes first: username `bot_user_<workspace id>`, display name Plane, an email built from `WEB_URL`'s host so two installations do not hand out the same address, and an administrator's membership of the workspace. The demo project takes the workspace's name, and its identifier is that name's letters and digits cut to five — spaces and punctuation are dropped rather than replaced, so "Acme Corp" gives "AcmeC".

The task does not run in a transaction and it re-raises rather than swallowing, so a failure part of the way through leaves everything written up to that point behind. That is kept: the errors on this path are returned.

Most of what a reader would get wrong here is in the models rather than in the task, because the seed calls each model's own `save` where a normal bulk load would not:

- **The states do not get the sequence the seed file asks for.** `State.save` throws it away when the project already has a state and uses fifteen thousand past the last one instead, so the file's 15000/25000/35000/45000/55000 land as 15000/30000/45000/60000/75000. It also fills in the slug, which a normal project's states — written with `bulk_create` — never get.
- **The cycles and the modules come out in the opposite order to the one the file numbers them in.** `Cycle.save` and `Module.save` both put a new one ten thousand *below* everything already in the project, so the file's 1, 2, 3 land as 1, −9999, −19999.
- **A work item's sequence number and sort order are both taken from the project rather than from the file**, which is what any other work item would get too.
- **Every seeded work item ends up with two sequence rows.** `Issue.save` writes one carrying the real number, and the task then writes another of its own that names no number at all, so it falls back to the column default of one. Reproduced rather than corrected.
- **The seeded pages open blank.** The file carries the editor's json document under `description`, and the task reads `description_json` — a key the file does not have — so every page stores an empty document. Its `logo_props` is never read either, so the emoji beside the page's name is lost.
- The display settings written for everybody are the task's own literals rather than the model's defaults, and they differ: the seed hides labels, modules, the cycle, the due date, the start date, the sub-issue count and the attachment count.

One thing that looks broken and is not: `IssueView.query` is a not-null column and the view seed never names it. `IssueView.save` fills it in from the view's own filters, and an empty filter set gives an empty object rather than running the filter translation, so the insert goes through.

## Migrated service: the leftover project invitation

`project_invitation_task` emails somebody an invitation to a project. Nothing in this edition queues it — invitations go out through `project_add_user_email_task` — and it is ported so that a message carrying its name does not sit unconsumed, and an installation with an older release still in flight is not left with a dead letter.

Its one side effect worth knowing is that the plain text of the email is written onto the invitation row before the message is sent, so an email that never goes out still leaves the copy behind. The invitor is named in the subject by whichever of their first name, display name and email address is set first.

## Migrated service: registering and configuring an installation

Three pieces that only make sense together: `manage configure_instance`, `manage register_instance`, and the telemetry task the second of them queues.

`configure_instance` writes one `instance_configurations` row per variable, taking each value from an environment variable. `internal/manage/instance_config.tsv` is the list of thirty-six, generated from `plane/utils/instance_config_variables` and regenerated by CI — it holds the *definition* rather than the value, because the value comes from the environment at run time. A row that already exists is left exactly as it is, so changing an environment variable after the first run does nothing until the row is removed. The eight encrypted ones go through `auth.EncryptConfiguration`, which is `encrypt_data`: a Fernet token under the key derived from `SECRET_KEY`, with an empty value stored as an empty string rather than as a token. Its round trip is tested against the reader the running server already uses.

`register_instance` records that the installation exists, or refreshes what is recorded, and then queues the telemetry push. Two details are worth knowing. `Instance.Meta` orders newest first, so `Instance.objects.first()` is the *newest* registration — and `create_instance_admin`'s `.last()` is therefore the *oldest*, which is corrected here from the earlier port. And the machine signature is taken as an argument, required when there is no registration yet, and then never written down.

## Migrated service: the telemetry push

`plane.license.bgtasks.telemetry_metrics.push_instance_metrics` reports how much an installation holds. `internal/worker/telemetry.go` is the port, on the official OpenTelemetry Go SDK against the same OTLP collector — gRPC by default, HTTP when `OTLP_METRICS_PROTOCOL` says so, and the same endpoint derivation: an https url with no port reaches an ingress on 443 and anything else a collector on 4317.

Two gates come first: an installation that was never registered reports nothing, and one whose telemetry is turned off reports nothing. Everything after that is swallowed, so an unreachable collector costs one log line and the next run tries again.

Nine gauges describe the installation and six describe each of its oldest thousand workspaces, read in a deterministic order so the same thousand is reported from one run to the next. The workspace counts are read in one statement rather than six per workspace, which is what the batched aggregation upstream is for.

The page count is the one that is not a plain count: it leaves out pages that are *both* owned by a bot and private. That is one condition rather than two, so a bot's public page is counted and so is a person's private one. Django renders it as an inner join and a negated pair, which is what the SQL here is — taken from the ORM rather than written by hand.

## Migrated module: the admin console

`internal/instances` is the god-mode API — the screens an operator signs into to configure the installation itself. Fourteen paths, twenty routes, and the last block of the app API that was still entirely Django's.

It is its own app with its own idea of who may call it. Being an administrator here has nothing to do with being an administrator of any workspace: the check is a row in `instance_admins` against the registration, with `role >= 15`. Fifteen is not a role the console ever hands out — it writes twenty — so the floor only matters for a row somebody wrote by hand.

The two sign-in routes are form posts that answer with a redirect and never anything else. Every refusal goes back to the console's own origin with `error_code` and `error_message` in the query string, because the console is a separate front end that reads its errors from there. The nine codes are the admin block of `AUTHENTICATION_ERROR_CODES`, 5150 to 5190.

`/api/instances/` is the one route here anybody may call, because the sign-in screen has to know what the installation offers before anybody has signed in. An installation with no registration answers two flags and nothing else, which is how the console knows to show the first-run screen.

Things worth knowing before reading a response from it:

- **`primary_owner_details` never appears.** `InstanceSerializer` declares it with `source="primary_owner"` and the model has no such field, so DRF drops it — the same rule that shortens the other serializers on instances. Twenty-two keys, not twenty-three.
- **`InstanceAdminMeSerializer` lists `is_email_verified` twice**, so it renders fifteen keys rather than sixteen.
- **`/api/instances/admins/session/` asks a different question from every other route here.** It checks only whether the caller administers *any* instance — no role floor, and without naming this one — so somebody left over from an earlier registration reads as signed in there and is refused everywhere else.
- **The configuration values are decrypted on the way out.** A client secret is sent in full to whoever is signed in as an administrator, which is what lets the console show it in a field.
- **Three of the config fallbacks disagree with what `configure_instance` seeds.** Signup falls back to off here and is seeded on; the magic link falls back to on here and is seeded off. It shows on an installation that has never been configured.
- **`disable-email-feature` writes plain empty strings into encrypted rows**, because it is one SQL `UPDATE` with a `CASE` rather than a save. The stored password becomes the literal empty string rather than an encrypted one. Nothing reads it afterwards, since the switch is off.
- **The first-run screen stores the telemetry answer as a boolean from whatever the form sent**, and any non-empty string is true — so `false` leaves telemetry on. Only an empty answer turns it off.
- `get_configuration_value` has two rules that are easy to get backwards. With `SKIP_ENV_VAR` set — the default — a row's value is used *even when it is empty*, so a setting somebody cleared reads as cleared. Without it the rows are ignored entirely and every value comes from the environment, so an installation configured through the console but running without that flag shows none of it.
- The two counts beside a workspace are correlated subqueries, so a workspace with no projects reports null rather than zero. The member count leaves out bots and anybody deactivated; the project count leaves out nothing.
- Deleting an administrator is a hard delete rather than a soft one, and answers 204 whether or not there was anything to delete.

The first-run screen takes the instance row's lock and re-checks the guard inside it, because two people submitting the form at the same moment could otherwise both become the first administrator. The guard asks whether *any* administrator exists rather than any of this instance, so a stray second registration cannot be used to get past it.

The credential check is the one place where the ported errors are coarser than Django's. Python's `smtplib` raises a different exception for each SMTP reply code and the view names each one; Go's client does not separate them the same way, so what is reported is the nearest of those sentences and, failing that, the one that covers the rest. The request fails either way, and with a sentence rather than a traceback.

## Migrated module: the five loose routes

Five routes that belong to nothing larger: the timezone list, the Unsplash search, the workspace's estimate scales, and the two assistant routes.

### Timezones

`GET /api/timezones/`, which anybody may read because the sign-up screen offers it. The pairs of friendly name and IANA identifier are copied into `internal/project/timezones.tsv` and CI diffs them against the endpoint that declares them; the *order* cannot be baked in, because it is by how far each zone is from UTC right now and that moves with daylight saving.

**A zone west of Greenwich on a half hour is reported an hour further out than it is.** The offset is worked out with python's floor division, so −9.5 hours floors to −10 and the remainder of 1800 seconds becomes 30 — and Marquesas, which is UTC−09:30, is reported as UTC−10:30. St John's has the same problem. East of Greenwich the arithmetic works, so Kolkata and Kathmandu are right. Reproduced rather than corrected.

The order is by `int(strftime("%z"))`, so a half-hour zone sorts thirty past its hour rather than halfway to the next one — the key is a four-digit number, not a duration.

### Unsplash

`GET /api/unsplash/` passes a search on and hands back whatever Unsplash says, status and body alike — including an error body, so a rejected key reaches the browser as Unsplash worded it. Without a key configured it answers an empty list rather than an error.

**Every search asks for the wrong page.** The url is built from a template literal whose dollar sign was never removed, so the page goes out as `page=$1` rather than `page=1`. Reproduced.

### The assistant

`POST` on `ai-assistant/` under a workspace and under a project. The project one names the project and the workspace beside the answer; the workspace one answers the text alone.

Three different misconfigurations — a provider nobody recognises, a missing key, and a model the provider does not list — all give back the same sentence, so an operator cannot tell which it was from the response. Every failure of the call itself answers the same sentence and a 500, so nothing about the provider reaches the caller either.

All three providers are called through the same OpenAI-shaped endpoint, because upstream points the OpenAI client at whichever one is configured. Gemini's model name is prefixed with the provider, which is the one thing that differs. The one deliberate addition is a thirty-second timeout, which `requests` does not have — without it a provider that never answers holds the request open.

### Workspace estimates

`GET /api/workspaces/<slug>/estimates/` starts from the projects rather than from the scales, so a scale nobody has selected is left out even though it exists, and a scale two projects share is listed once. Any active member may read it, guests included: the permission only narrows on the unsafe methods and this route has none.

## Migrated module: the v1 asset routes

Ten routes that upload *through* the API rather than handing the browser a signed policy. Everything the editor writes today goes through the v2 routes, where the browser posts straight at the bucket; these take the bytes themselves, and they are still here because older clients still call them.

Seven of them are the general file assets — a workspace's, and a person's own — and three are the work item attachments under their old path.

The order inside an upload is upstream's and worth knowing: the object is written to the bucket first and the row second. An upload that fails at the row therefore leaves an orphan in the bucket that nothing will ever point at, and there is no sweep for those — the sweep that exists looks for rows without objects, not objects without rows.

The two paths differ from their v2 neighbours in more than the transport:

- The v1 attachment list shows **every** attachment; the v2 one narrows to the ones that finished uploading. A row whose upload was started and abandoned appears in one and not the other.
- The v1 attachment delete **really removes** the row, and takes the object out of the bucket on the way. The v2 one flags the row and leaves the object for the storage sweep. A mistaken delete on the old path cannot be undone.
- A v1 row is marked uploaded on the way in, because the bytes are already there. A v2 row waits for the browser to say so.

Two things about the general file assets are worth stating plainly. Looking one up by key names nothing but the key, so any member of any workspace can read the row of an asset in another as long as they know its key — that is the route as it stands. And not finding one is a `200` carrying `{"status": false}` rather than a `404`, which is what the client reads.

`GET /api/users/file-assets/<key>/` answers one object rather than a list even though it filters a queryset, because the serializer is handed the queryset without `many` — Django renders that as the first row's fields, which is what this answers.

The delete and restore routes set `is_deleted` rather than `deleted_at`, so the row stays where it is and the object stays in the bucket. That is what the restore route depends on.

## Migrated: the DRF format suffixes and the router root

Thirteen routes that were listed as deliberate omissions and are now served.

Two of the external API's viewsets — the workspace invitations and the stickies — are mounted through a `DefaultRouter` rather than with plain paths, and a router appends a format suffix to everything it registers. So `stickies.json` and `stickies/<id>.json` are real routes while `states.json` is a 404, and that asymmetry is DRF's rather than anything about the resources.

A suffix cannot be a path parameter in this router, because a parameter is a whole segment and a suffix is part of one. So the rewrite happens where a request has already failed to match: the suffix is stripped, the path is dispatched again, and it lands on the route that was always there. A second pass cannot rewrite again, because the suffix is gone.

A suffix other than `json` is a `404` rather than a `406`, which is what content negotiation raises when no renderer answers to it — and `JSONRenderer` is the only one configured.

The router's own root, `GET /api/v1/workspaces/<slug>/`, answers one key and one absolute url. Two routers are mounted at the same prefix, so only the first one's root ever resolves — and the first is the invitations one, which is why the stickies are not listed beside it. It is also **the one route under `/api/v1/` that an api key cannot reach**: DRF's own `APIRootView` takes the project's default authentication, which is the session, rather than the key authentication every view in that package declares for itself.

The cutover guard is taught these shapes explicitly, because they really are served but not by a route of their own.

## Migrated module: the archived cycle and module details

The two detail routes under `archived-cycles/<id>/` and `archived-modules/<id>/`, which the archive screens open a card into.

Each answers the archived projection plus both distributions: `distribution`, which counts work items per assignee and per label, and `estimate_distribution`, which sums points the same two ways. The point one is an empty object unless the project measures in points at all, and each carries a completion chart that is drawn only when both of the dates are set.

**A cycle or module that is not archived is a 500, not a 404.** Both querysets filter `archived_at__isnull=False` before the id is applied, so a live one gives nothing back and the view then subscripts that nothing. Reproduced rather than corrected: a 404 here would be a different answer to the same request.

The two projections differ from the live details in ways the archive screens depend on. The cycle's is the archived list's twenty-three fields plus five — `sub_issues`, `logo_props`, the two point totals and `created_by` — and it renders its dates in UTC rather than in the project's zone, because nothing on this path converts them. The module's is `ModuleDetailSerializer`: the module serializer's fields plus the nested links, the sub-item count, and the four per-state point sums.

The two distributions are not shaped alike either. A cycle's assignee rows carry a display name; a module's carry a first and a last name *and* a display name, and are ordered by the first name — so somebody with no first name sorts to the top rather than under their display name. That difference is upstream's, and the frontend reads both shapes.

The counting rules are the ones already documented for the cycle analytics route and apply here unchanged: each count is over the grouping column rather than over the row, so the bucket holding work items with no assignee counts **zero** of them; and a sum with nothing to add is null rather than zero, so a bucket whose work is all still open reports `null` completed points.

## Every route the Django app serves is now served here

`TestEveryCutOverPathIsFullyServed` compares three sources — the routes Django binds, the paths the proxy cuts over, and the routes this router registers — and there is no longer a Django route without a Go route behind it.

The proxy cuts 634 of the 639 over. The five it does not are one shape rather than five paths: the fixture collapses any segment carrying a star into a bare one, so `stickies.json`, `invitations.json` and the router root with a format suffix all read as `/api/v1/workspaces/*/*`, and a collapsed probe cannot match a matcher written against the real shapes. The real paths are cut over and are checked as such by `TestCommunityProxyCutsOverOnlyTheExternalStickyAndInviteRoutes`.

What is left of the migration is not the API. It is the `live` service — the Node process behind the collaborative editor — and the first piece of it is below.

## The live service, part one: reading a page out of Yjs

A Plane page is not stored as HTML. It is stored as a Yjs document — a CRDT update, `description_binary` on the row — and the HTML and JSON columns beside it are derived from that update every time the page is persisted. The live service is what derives them, so any Go replacement has to start by reading the same bytes into the same document. `internal/ydoc` does that.

There is no CRDT here. `github.com/reearth/ygo` is a Go port of Yjs with a byte-level conformance suite against `yjs@13.6.31`, and it is a dependency rather than a rewrite. What this package adds is the layer above it: y-prosemirror's reconstruction of a ProseMirror document from a Yjs XML fragment, against Plane's own editor schema.

### The schema is generated, not transcribed

Reconstruction is `schema.node(name, attrs, children)` for every element and `schema.text(text, marks)` for every run of text, and both need to know the schema: which attributes each type declares, what each defaults to when the document does not carry it, and what order the mark types were registered in — because that order is the rank ProseMirror sorts a text node's marks by, and it is the reason `bold` precedes `italic` in the output no matter which order they were applied in.

None of that is behaviour, so none of it is hand-written. `tools/generate_ydoc_schema.mjs` builds the real schema from the real extension list and dumps it to `internal/ydoc/schema.json`, which the package embeds. Twenty-three node types and eight mark types, and CI fails if the file drifts from the editor.

The editor package will not build in this tree — `@plane/propel` has an import of a directory that does not exist — so the generators bundle the schema path directly with the workspace UI packages replaced by a proxy. Nothing replaced is ever called: building a schema reads names, attributes and renderers, and never renders a node view.

### An attribute has three states, not two

This is where the port was wrong first, and the corpus is what caught it.

A declared attribute with no default has to be carried by the document; ProseMirror rejects a node that omits one. An attribute defaulting to `null` is satisfied without the document carrying it, and the null is stored and serialised. But several extensions — the work item embed, the callout — declare `default: undefined`, which is also satisfied without the document carrying it, and then holds no value at all: `JSON.stringify` drops the key, so the attribute is simply **not there** in `description_json`. Collapsing that into `null` puts three keys into an embed's attrs that the editor never wrote.

The same distinction shows up one level out. A node type that declares no attributes at all produces no `attrs` key; a type that declares attributes always produces the key, even when every one of them came out undefined and it is left empty. So `Node.Attrs` distinguishes nil from empty, and marshals accordingly.

A third rule follows from the writers rather than the readers, and it holds for a node but not for a mark. **A node's Yjs attribute is never null**: both of y-prosemirror's write paths skip the key rather than write one, so anything that decodes to nothing was JavaScript's `undefined` and is read as "not supplied" — which is why a table cell handed `colwidth: null` comes back carrying the schema's `[150]`. A mark is not stored that way at all. It is a formatting attribute on the text, and its whole attribute object is handed over as one value, nulls and all, so a link with `class: null` comes back with `class: null` rather than with the class the schema defaults to.

### The title is a document, not a string

A page's title lives in a second fragment of the same update, and it is not text: it is a whole ProseMirror document, a level-one heading holding the title. `ParseTitle` returns that document; flattening it to the string the API stores is `TextContent`.

### What the corpus is

`internal/ydoc/testdata/documents.json` is forty-three documents run through the real editor by `tools/generate_ydoc_fixture.mjs`, recording for each one the Yjs update bytes, the ProseMirror JSON, the title document and the HTML. Every node type and every mark type in the schema appears in at least one of them, and a test fails if one stops appearing.

Thirty-one cases are written as the HTML a user would paste. Twelve are written as ProseMirror JSON instead, for the shapes no HTML parses back into: an emoji only ever enters a document through the `:shortcode:` input rule, `colspan` and `rowspan` only ever become non-default through the table toolbar, and a `javascript:` href cannot be pasted in because the parser refuses it on the way through.

Yjs draws a random client id for every document it creates, so the same input produced different update bytes on every run and the fixture could not be diffed. The generator replaces lib0's randomness with a counter, which touches client ids and generated element ids and nothing the fixture exists to pin down. CI regenerates the file and diffs it.

### What is not reproduced

ProseMirror validates a node's children against its content expression, and y-prosemirror responds to a failure by **deleting the offending element from the document** and carrying on. That is not implemented here: this package checks that a node type exists, that a mark type exists, and that every attribute without a default was supplied, and returns an error rather than dropping anything. The difference is only reachable with a document whose structure the editor could not have produced.

## The live service, part two: rendering the page back to HTML

The other two columns a page keeps, `description_html` and the page's title, are the same document rendered. The renderer is ProseMirror's `DOMSerializer` over the same schema, with zeed-dom's markup rules underneath it — the two the editor reaches through `@tiptap/html` — and every extension's `renderHTML` ported beside them. Twenty-three node types, eight mark types, checked against a corpus that has now grown to forty-three documents.

### Every style attribute is silently dropped

This is the finding worth reading twice.

ProseMirror's `renderSpec` treats `style` differently from every other attribute: rather than `setAttribute`, it assigns `dom.style.cssText`. The DOM this pipeline runs against is zeed-dom, whose `style` property is a getter that builds a fresh object out of the current attribute and returns it. Assigning to that object writes to something nobody will read, and the attribute is never set.

So **no style survives into `description_html`**. Text alignment does not: a centred paragraph is stored as a plain `<p>`. A coloured table row does not, nor a cell background, nor a custom text colour outside the editor's named palette. Each of those extensions computes a style declaration on every render, and each declaration goes nowhere.

The port reproduces it, dropping `style` at the point ProseMirror does and building the declarations anyway, because that is what the extensions write and a change upstream should show up as a corpus diff rather than as silence.

### The rest of what the corpus pinned down

- **Escaping is zeed-dom's, not Go's.** An apostrophe is `&apos;` where Go's `html` package writes `&#39;`, a non-breaking space is `&nbsp;` and a soft hyphen is `&shy;`. The same escaping covers text and attribute values.
- **A boolean attribute is a bare name or nothing at all.** A ticked task item carries `data-checked` with no value; an unticked one carries no `data-checked`.
- **A self-closing tag has no closing tag.** `<br>` and `<img>` are written open and left that way, which is zeed-dom's list rather than HTML's.
- **The nesting of marks follows the schema, not the document.** A text node listing `italic` before `bold` still renders `<strong><em>`, because ProseMirror sorts a mark set by the order the types were registered in. A mark shared by adjacent text nodes opens once and wraps them all.
- **A `null` attribute beats the extension's default.** A link mark carrying `target: null` renders with no `target`, even though the extension's options supply one, because Tiptap's `mergeAttributes` lets the later value win. A `null` *class* is the exception: the class branch merges lists and an empty one leaves the existing classes alone.
- **A list starting at one loses its `start`.** Every other start is written out.
- **A heading's level and a code block's language are never attributes.** The level picks the tag, the language picks the inner `<code>`'s class.

### A dangerous href is emptied, not dropped

The link extension refuses `javascript:`, `data:` and `vbscript:` by rewriting the href to the empty string, leaving the text as a link that goes nowhere. The check normalises first, the way a browser does — the WHATWG URL parser strips tab, newline and carriage return from anywhere in a url and strips leading C0 controls and whitespace before the scheme — so a leading tab does not slip a `javascript:` past it.

### The title is rendered, not read

`titleHTML` is not the title's text. The editor renders the title document to HTML and runs the result through a sanitiser with every tag disallowed, and that round trip is not the identity: the sanitiser decodes the entities the renderer just wrote and escapes a narrower set on the way back out, then trims.

What comes out is the title with `&`, `<` and `>` escaped and nothing else — a quote and an apostrophe survive as themselves, and so do a non-breaking space and a soft hyphen, all of which the renderer had escaped. A title somebody typed `&amp;` into comes back as `&amp;amp;`.

### The emoji table is generated

An emoji node stores a shortcode and nothing else; the character comes from a lookup against the list the editor is configured with, which is GitHub's set minus the entries that have no character. `tools/generate_ydoc_emoji.mjs` flattens that list into `internal/ydoc/emoji.tsv` — 2,832 keys, since a name and each of its aliases both resolve — and CI diffs it. A shortcode the table does not know renders back between colons.

## The live service, part three: the process itself

`cmd/live` is the Go replacement for the Node process. This is its skeleton — the HTTP surface, the configuration, the client it talks to the API with, and the wire protocol its websocket speaks. The websocket endpoint itself is the next piece.

### It runs as the user, not as itself

The service holds no credentials of its own for reading or writing a page. Every call it makes to the API carries the editing user's session cookie, so the API's permissions are what decide who may read and write. Authentication here is only the check that the cookie really belongs to the user the connection claims to be: the token is JSON carrying an id and a cookie, `/api/users/me/` is asked whose cookie it is, and the two ids have to match.

The token carries the cookie because a browser opening a websocket to another origin does not reliably attach one. A token that will not parse is not fatal on its own — the cookie then comes from the request headers — but a connection that ends up with no user id is refused, and the user id only ever comes from the token.

The client refuses to follow a redirect. The asset routes answer with a 302 into object storage, and following it would send the user's session cookie to a third party.

### helmet and cors are ported rather than approximated

Both are middleware the browser depends on, so both are written out header by header.

The security headers are helmet's defaults, read off helmet 7.2: a content security policy with the eleven directives helmet ships, `Cross-Origin-Opener-Policy` and `Cross-Origin-Resource-Policy` at `same-origin`, `Origin-Agent-Cluster: ?1`, `Referrer-Policy: no-referrer`, HSTS at a hundred and eighty days, and the five `X-` headers. `Cross-Origin-Embedder-Policy` is deliberately absent, because helmet has not set it by default since version five.

CORS is the `cors` package's behaviour for the way this service configures it. The allowed origins are a list, so an allowed request gets its own origin reflected back and a disallowed one gets no `Access-Control-Allow-Origin` at all — the request is still served, and it is the browser that refuses the answer. `Vary: Origin` goes out either way. A preflight is answered here and never reaches a route: `204` with an explicit `Content-Length: 0`, which Safari needs before it will stop waiting for a body, and that happens for any `OPTIONS` request including one to a path nothing serves.

**With `CORS_ALLOWED_ORIGINS` unset, nothing is allowed.** Splitting an empty string on commas gives one empty entry rather than none, so the list has a member and no real origin matches it. That is the upstream behaviour and a deployment that has not set the variable is relying on it.

### Every route answers with or without its trailing slash

Express matches both spellings and answers both the same way; Gin would redirect. Each route is registered under both.

### The wire protocol

`internal/live/hocuspocus` is the framing the client speaks. It is not plain y-websocket: every frame carries the document's name in front of it, and there are message types beyond the two y-protocols defines — authentication, stateless signals, a save acknowledgement and a heartbeat. The encoding underneath is lib0's, unsigned LEB128 for a number and a length-prefixed run of bytes for everything else.

`tools/generate_hocuspocus_frames.mjs` builds sixteen frames with the real server's own encoder and records them, and the Go side has to produce the same bytes. The corpus covers the cases a hand-written varint gets wrong: a document name long enough to need a two-byte length, a name and a payload with characters outside ASCII, and an empty payload.

## The live service, part four: the grammar of a document

Reading HTML back into a document needs something the renderer did not: the schema's **content expressions**, the grammar that says a document holds blocks, a list holds list items, a list item holds a paragraph and then any blocks, and a table row holds cells and nothing else.

ProseMirror compiles each expression into a finite automaton, and `internal/ydoc/content.go` is a port of that compiler — the tokeniser, the expression parser, the NFA, the subset construction that makes it deterministic, and the four questions the parser asks of the result.

### The edge order is the behaviour

The automaton is not just a membership test. The order of a state's outgoing edges is what decides which type gets *invented* when something has to be filled in: a document asked for empty comes back holding a paragraph because `block+`'s first edge is the paragraph's, and a bare list item at the top of a document is wrapped in a bullet list rather than an ordered one for the same reason.

So the port is checked against ProseMirror's own dump of the automaton. `ContentMatch.toString()` prints every state, whether a node may end there, and every outgoing edge with the state it leads to — and two automatons that print alike are the same automaton. All twenty-three of the schema's types print identically, as do seventeen expressions the schema itself never contains, which are there to exercise ranges, optionals, alternation and nesting.

Beside the dumps the corpus records the answers themselves, for every pair of types in the schema: 529 `matchType` answers, 529 `findWrapping` answers, and the node each type builds when asked for one empty — which is recursive, so a table arrives already holding a row.

### What the four questions are for

- **`MatchType`** — may this child go here? The parser asks it for every element it reads.
- **`FindWrapping`** — what would this child have to be wrapped in to go here? This is how a `<li>` pasted at the top level becomes a list, and how a `<td>` becomes a table.
- **`FillBefore`** — what would have to be inserted for this to be whole? This is how an empty list item ends up holding a paragraph.
- **`DefaultType`** — what belongs here when nothing else says? This is the type a gap is filled with.

### The mark set is the other half

A type also says which marks its content may carry, and the rule for a type that says nothing depends on what it holds: a type holding inline content allows every mark, and a type holding blocks allows none, because the marks belong to the text inside those blocks rather than to the block itself. The code block is the one type that names its own — the empty expression, meaning no mark at all, which is why pasting bold text into one loses the bold.

## The live service, part five: the HTML parser underneath

`internal/vdom` is the document object model the editor reads HTML into. The important thing about it is what it is **not**: it is not an HTML5 tree builder.

The editor parses with zeed-dom, whose parser is a scanner over the markup with a stack of open elements and none of HTML5's repair rules. That has consequences a page runs into:

- **A closing tag pops whatever is open, not the tag it names.** `<b><i>both</b>italic</i>` nests by where the tags are rather than by what they say, so the italic ends up inside the bold.
- **Nothing is implied.** A `<tr>` written straight inside a `<table>` stays there; no `<tbody>` is invented around it. Every parse rule for a table has to cope with that.
- **An unbalanced closing tag throws the rest away.** `</div>` with nothing open pops the fragment itself off the stack, and from then on there is nothing to append to — so `<p>text</p></div>more` parses to the paragraph alone and `more` is simply gone.
- **Markup it cannot make sense of is text.** An unterminated comment, a `<` that starts nothing, an unterminated `<script>` — all text.
- A `<b>` answers "bold" when asked for its font weight even with no style attribute, because zeed-dom hands a handful of tags presentational defaults. Two of those defaults are misspelled — the decoration property is in the plural — and that is reproduced rather than corrected, because a rule matching the real spelling finds nothing there either.

Using Go's own HTML5 parser would have repaired all of that, and repaired documents are different documents.

### The corpus

`tools/generate_vdom_fixture.mjs` records the tree zeed-dom builds for 75 inputs: 32 written to provoke the behaviours above, and every one of the 43 documents the renderer produces, fed back in. That second half is the input shape that matters most — a page's stored HTML is the renderer's own output, and it is exactly what gets parsed back when somebody opens a page that has no Yjs document yet.

## The live service, part six: reading HTML back into a document

`internal/ydoc`'s parser is ProseMirror's `DOMParser` over the schema's own rules, with the HTML scanner above underneath it. It closes the loop: a page's stored HTML goes in and the document the editor would have made of it comes out.

### What is generated and what is written

The rules are data and are generated. `tools/generate_parse_rules.mjs` dumps all 46 of them — the selector, the priority, what each produces, the flags that change how its content is read, and the ordered list of attributes each type reads off an element.

What is not data is the code a rule may carry. Eleven rules have a `getAttrs` of their own and eleven attributes have their own reader, and each of those is ported by hand. The generated file records the **JavaScript source of every one of them** beside the entry, so the port can be read against the original and a change upstream lands as a diff. Three tests keep the two halves in step: every rule shape has to be one the parser implements, every hand-written function has to still have a rule, and every attribute the editor reads with code has to have a reader here.

### Three upstream behaviours, reproduced

The corpus found these; none of them is what the code looks like it does.

- **Every `<span>` becomes a colour mark.** The two `customColor` rules are written as `node.getAttribute("data-text-color") && null`, which is meant to refuse a span that carries no colour. The DOM the editor runs on answers `undefined` rather than `null` for a missing attribute, `undefined && null` is `undefined`, and Tiptap only treats a literal `false` as a refusal. So both rules match every span unconditionally, and a plain span in a page's HTML comes back carrying a `customColor` mark with both colours null.
- **A code block's language is never read back.** The reader looks for a `language-` class on the element's `firstElementChild`, and that DOM has no such property. The lookup yields nothing every time, so a code block whose HTML says `language-go` returns with no language and the highlighting is lost the first time a page is read back from its HTML.
- **An empty `<span data-type="emoji">` is dropped.** It is a consequence of the first one: the colour rule matches the span first, a mark rule parses the element's *children*, and an emoji span has none.

### The editor is not idempotent

Reading a page's HTML and rendering it back does not always give the same HTML. `<span style="font-weight: bold">` is read into a bold mark plus a colour mark; those render as `<span><strong>`; and reading *that* gives the marks in the other order, which renders as `<strong><span>`. The second pass is stable.

The corpus records both renderings and both readings for all 96 documents, so the port is checked to be un-idempotent in exactly the same way rather than merely checked to be stable.

### The corpus

96 documents: 52 written to work the parser — content in the wrong place, elements with no rule, whitespace, overlapping tags, styles that cancel marks — and every one of the 43 the renderer produces, read back in. Each records the document it parses to, the HTML that renders to, the document *that* parses to, and the HTML that renders to.

### The selector subset

Parse rules match with CSS selectors, and the ones this schema uses need a tag name, `[attr]`, `[attr="value"]`, `[attr^="value"]` and `:not(...)`. Exactly that is implemented, and anything else is refused at load rather than silently matching nothing — so a rule added upstream with a selector this does not understand fails where somebody will see it.

## The live service, part seven: writing a document back out as Yjs

`ToUpdate` is the last direction: a document goes in and the Yjs update a page is stored as comes out. With it the loop closes — a page that has HTML and no Yjs document gets one, and every client that connects afterwards synchronises against it.

### What is checked, and what cannot be

The corpus records the update the editor writes for all 97 documents, with both sides fixing the document's client id so the bytes are comparable at all. Every one of them reads back to the same document. Most of them are also **byte for byte** the same update.

The ones that are not differ for three reasons, all of them in the Yjs port underneath rather than in anything above it, and none of them changing the document that comes back. `EncodesLikeTheEditor` names all three, and the test checks both halves: a document that should match has to, and one that should not has to actually differ, so the exemption cannot quietly grow.

- **An attribute whose value is JavaScript's `undefined`.** The editor writes one for every attribute a type declares with an undefined default that the document does not carry, because `if (val !== null)` lets `undefined` through where it stops `null`. The port has `null` and nothing else. Reading back maps both to nothing, the schema's default applies, and the default is `undefined` again.
- **A run of text whose marks are not in alphabetical order.** The editor opens the formatting markers in the order the marks are in, which is the schema's; the port sorts them by name, deliberately, because the order they are linked in is observable.
- **A mark attribute holding `&`, `<` or `>`.** A mark's attributes travel as JSON and the port writes that JSON with Go's encoder, which escapes those three so its output is safe to drop into a page. JavaScript's leaves them. A link whose address carries a query string is where this shows up.

### Three things the port had to get right

Each of these was found by the corpus rather than by reading.

- **A whole number is written as an integer, not as a float.** JavaScript has one number type and Yjs writes a whole one as a varint; Go has both, and a number that arrived through JSON is a float whatever it holds. Handing a whole one over as an integer is done at the boundary where the two languages meet.
- **A run of text is written as one delta, not as a sequence of inserts.** The two produce the same text and different bytes: a delta walks a cursor to the end and appends, where an insert at a position points at what follows it, and the items then carry a right neighbour the editor's never do.
- **A node's attributes go in in the schema's declared order.** The order is not decoration — two documents whose attributes went in in different orders are different bytes — so the schema dump now carries the order and the writer follows it.

## The live service, part eight: the collaboration endpoint

`/live/collaboration` is the websocket every open page is connected to, and the reason the service exists. With the four content directions in place it is mostly bookkeeping — but the bookkeeping is where the behaviour is.

### One socket, several documents, and nothing known until the first frame

The page's id travels in the frames rather than in the url: a client connects to one endpoint and puts the document's name in front of every message. So a connection is not for anything until its first frame arrives, and a socket may end up carrying several documents at once.

That is also why authentication is a message rather than a header. A client sends its token and its first sync step back to back without waiting, so **everything that arrives before the token is queued** and replayed once the document is open. Getting that wrong looks like a page that never loads for exactly the clients that are fastest.

### It runs as the user

Every read and write of a page is made with the editing user's session cookie, so the API decides who may open what. The service's own check is only that the cookie belongs to the user the token names. A refusal is sent back as a permission-denied message and **the socket is left open**, which is what the client expects: it shows the failure and decides for itself whether to retry.

### A page with no document is built from its HTML

Every page created through the API has HTML and no Yjs document. The first person to open one gets it built from the page's HTML and title, and the result is **written straight back** — so the conversion happens once rather than on every open.

### Saving

A change schedules a save ten seconds out, and because the maximum wait is also ten seconds a page being typed into continuously is written on a fixed cadence rather than never. The last connection off a page writes it immediately rather than leaving it on a timer, and a page nobody changed is simply let go.

A failed save is **told to the people editing**, not just logged, because the alternative is somebody typing into a page that is no longer being saved. A page the API refuses as too large is the one failure there is no way forward from: everybody on it is told why, given a moment to read it, and disconnected, and the document is released rather than left collecting changes nothing will ever write.

### Cursors leave with the people they belong to

An awareness update names the client ids it speaks for, and those are remembered against the connection that sent them. When the connection goes, an update is broadcast that raises each of those clients' clocks and blanks its state — which is how y-protocols says somebody left, and what takes their cursor off everybody else's screen.

### What is not here yet

The relay that makes this work across more than one server is below.

## The live service, part nine: carrying a page between servers

Two people editing the same page do not necessarily reach the same server. `internal/live/relay.go` is what makes that work: one Redis channel per document, carrying **the same frames the clients send**. A server relaying to another is doing what a client does, from the other side.

### Announcing a change does not send it

The exchange is not obvious and getting it wrong is silent. A server that has applied a change publishes **its own state vector** — which on its own tells the others nothing about the change. Each of them answers with two things: what the first server is missing according to them, *and* their own state vector. It is the answer to that second half that carries the change back.

The first version only did the first half. Everything looked connected and nothing ever crossed.

### A publisher is delivered its own messages

Redis has no way to be asked not to, so every message carries the sending server's name in front of it and a server drops its own. Applying one's own change twice is not harmless: it is applied to a document that already has it.

### Only one server writes the page

Every server serving a page holds the whole of it, so without a lock they would all write it and the last write would win a race nobody needed to run. The lock is held only for the write and expires on its own, so a server that dies holding one does not stop the page being saved — and releasing it only succeeds if it is still ours, because a lock that expired and was taken by somebody else is not ours to give away.

If Redis cannot be reached the page is written anyway. The worst case is the write another server was also making; refusing to save would be worse.

### A page nobody can save is closed everywhere

A page the API refuses as too large is closed out from under everybody on every server serving it, not only the one that tried to write it, over a channel of its own.

### The document's lifetime is one lock

Two sockets closing at the same moment both decide the page is theirs to finish with, and the first version got this wrong twice — once writing the page out **empty**, because one goroutine read the document out while another had already let it go, and once **losing the change entirely**, because the one holding the pending save found the document destroyed.

The shape that works: the waiting change is claimed out of a map before anything else, so exactly one caller writes it; and everything that writes the page or takes it out of memory holds the document's own guard. That also means a force close has two entry points — one for the save that is already holding the guard, and one for a command arriving from another server, which is not.

### Without Redis

`REDIS_URL` and `REDIS_HOST` unset means the service runs as a single node and every relay call is a call on nothing. Two people on one page then have to reach the same server for it to converge, which is what the deployment is choosing when it leaves those unset.

## The live service, part ten: converting a document without a connection

`POST /live/convert-document/` is the one route here that is not about a live connection. The API calls it when it copies a page or a work item description: the assets in the HTML have been swapped for their copies, and the Yjs document and the JSON beside it have to be rebuilt from the result.

### There are two schemas

The editor has two. A page is written with the document one; everything else — a work item's description, a comment — with the rich text one, which is the same extension list **without the work item embed**. That single node is the whole difference, and it is a real one: the same HTML converted as a page keeps its embed and converted as a description loses it.

Both are generated, both are diffed by CI, and a test pins the disagreement, because it is the reason there are two.

**The caller asks for them the other way round.** The API sends `"rich"` for a page and `"document"` for everything else. That is upstream's, reproduced rather than corrected.

### It is not a parse and a render

The HTML is read into a document, the document is written out as a Yjs update, and the three representations are then derived from **that update** — not from the document that was read. A document read out of an update is not always the document that went into it, and the route's answer follows the longer path because the editor's does.

Only two of the three go back. The caller keeps the HTML it sent, even though converting it changes it, which is what keeps a copy's HTML identical to the original's.

### The binary is different every time

A Yjs update carries the client id of whoever wrote it and the route draws a fresh one on every call, deliberately, so that two servers converting the same content do not claim the same identity. So the corpus compares the update as the document it holds rather than as bytes, and compares the JSON and the HTML exactly.

## The live service, part eleven: drawing a page as a PDF

`POST /live/pdf-export/` draws a page. It is the one piece of the whole migration whose output **cannot** be reproduced exactly, and the reason is worth stating plainly rather than discovering later.

The original lays a page out with react-pdf, which is a flexbox engine. `internal/pdfdoc` flows blocks down the page instead. The same six faces of Inter are embedded — unpacked from the very WOFF files the exporter registers, because where a line breaks depends on the width of every character — and every measurement, colour and spacing is read out of the exporter's own stylesheet rather than transcribed. A page therefore looks like the same page. It is not the same bytes, and a long document may break across pages differently.

Every other piece of this migration is checked against the original's own output. This one cannot be, so what is checked instead is that **every document the editor can produce draws**: all forty-three of the corpus, plus the six page sizes and two orientations, a document long enough to run onto a second page, a mention with and without a name for it, an image that will not decode, and the asset scan that decides what has to be fetched first.

### Two things it had to work around

- **A character outside the basic multilingual plane brings the PDF writer down.** It keeps one entry per character code in a table of sixty-five thousand and indexes it directly, so an emoji walks off the end of it. The font has no glyph for one either, so the text is folded to that range before it is drawn: nothing is lost that would have been drawn, and a page with an emoji in it exports rather than crashing the process.
- **A request naming no project gets a 500, not a 400.** The request schema says the project is optional and the page service it reaches throws without one; upstream that throw is a defect rather than a failure, so it lands on the same five hundred any unexpected failure gets. Reproduced rather than turned into the four hundred it looks like it should be.

### Nothing in this repository calls it

The web app renders its PDFs in the browser with react-pdf, and no other caller exists here. The route is ported because it is part of the service being replaced and something outside this repository may reach for it — not because anything inside it does.

## The live service, part twelve: the cutover

The eleven parts above build a service. This part is the one that makes the Node one stop running.

`docker-compose.yml` still has a service called `live`, and the container is still called `plane-live`. Only the build changed: `./apps/api-go` with `entrypoint: ["pace-live"]` instead of `./apps/live/Dockerfile.live`. The names are deliberate. `apps/proxy/Caddyfile.ce` routes with `reverse_proxy /live/* live:3000`, and the web and admin apps are handed a `LIVE_URL`; renaming the service would mean editing both, so it keeps its name and neither changes.

It replaces the Node service rather than running beside it. Two servers holding the same page converge only through the Redis relay of part nine, and the proxy sends every `/live/*` request to one host anyway — so running both would mean some editors on one server and some on the other with no path between them. Reverting is switching the build back to `./apps/live/Dockerfile.live`, which is still in the tree.

The image now carries five binaries rather than four, and its base tag moved from `golang:1.25-alpine` to `golang:1.26-alpine` — the `go` line in `go.mod` had already moved to 1.26, and since the image pins `GOTOOLCHAIN=local`, `go mod download` was refusing to run rather than fetching a newer toolchain. The image had not been buildable since; it is now, and `pace-live` was run out of it and asked for a page conversion to confirm the binary in it works.

### What is not switched

`deployments/` is untouched, and nothing there runs the Go live service:

- `deployments/cli/community/docker-compose.yml` still pulls `makeplane/plane-live:${APP_RELEASE:-stable}`.
- `deployments/aio/community/` still pulls that same published image and its `supervisor.conf` still runs `node /app/live/apps/live`.

This is not an oversight in this change. No Go service is wired into `deployments/` at all — not the API, not the worker, not the beat scheduler — because those directories consume published `makeplane/*` images rather than building from this tree. Switching them needs a published image to switch to, which is a release concern and not a code change.

## The beat, cut over

`beat-go` now runs and `beat-worker` is gone. The two could not have run side by side: both read and write `django_celery_beat_periodictask`, so the pair would have queued all twelve periodic tasks twice. It keeps its own container name rather than taking over `beatworker`, since nothing addresses the scheduler by name — it talks to Postgres and RabbitMQ and nothing talks to it.

Running it against a database carrying the real `django_celery_beat` schema found something reading the code had not. A task whose `last_run_at` is null was treated as due immediately, so the first time beat ever started it fired the whole schedule at once — `hard_delete`, `archive_and_close_old_issues` and the ten others, all in the same second.

Django does not do that. `ModelEntry` fills a null `last_run_at` in from `date_changed`, which `auto_now` holds at the moment the row was last written, so a task the scheduler has only just created waits a full period before its first run. The exception is a task with a start time, which is backdated by thirty years instead — that makes the schedule due whatever it is and leaves the separate start-time gate to decide, which is how such a task fires *at* its start time rather than at the first slot after it.

Both branches were read off django-celery-beat itself rather than off its source: three rows were built in a Django-migrated database, `last_run_at` forced back to null, and `ModelEntry.is_due` asked directly.

| row | Django | Go, before | Go, now |
| --- | --- | --- | --- |
| never run, no start time | not due | due | not due |
| never run, start time an hour ago | due | not due until midnight | due |
| never run, start time in an hour | not due | not due | not due |

The end of it, on a database truncated back to empty: beat syncs twelve entries and queues nothing. Set one task's `last_run_at` two days back and it queues that one, on `pace-go`, and nothing else.

## The API, cut over

The proxy's last two lines pointing at Django now point at Go, and the `api` and `worker` services are gone from the compose file. What is left of Python is `migrator`.

The fallback was already dead weight before it moved. `reverse_proxy /api/* api:8000` sat below a hundred-odd matchers that had claimed every route the app has: all 639 rows of `internal/server/testdata/django_routes.tsv` match something above it. Moving it changes what answers a request for a path that exists in neither app.

Moving it also sharpens the guard. `TestEveryCutOverPathIsFullyServed` requires that every method Django serves on a cut-over path is registered on the Go router; with `/api/*` claiming the whole subtree, that is now every method of every route, rather than only those a named matcher had named.

### Two different 404s, and Go was writing the wrong one

Django answers a request that resolved to nothing with `handler404` — `plane.app.views.error_404.custom_404_view`, which writes `{"error": "Page not found."}`. DRF's `{"detail": "Not found."}` is a different thing: it needs a view to have been found and to have raised `Http404`, which here happens exactly once, when a format suffix names a format no renderer answers to. Go was writing DRF's body for both.

Both were read off a running Django with `DEBUG` false — which matters, since with it on the debug page is served instead and the handler never runs — and then off the Go service standing in front of the same migrated database:

| request | Django | Go |
| --- | --- | --- |
| `GET /api/nope/` | `{"error": "Page not found."}` | same |
| `GET /api/v1/nope/` | `{"error": "Page not found."}` | same |
| `POST /api/workspaces/acme/nope/` | `{"error": "Page not found."}` | same |
| `GET /api/v1/workspaces/acme/states.json` | `{"error": "Page not found."}` | same |
| `GET /api/v1/workspaces/acme/stickies.xml` | `{"detail":"Not found."}` | same |
| `GET /static/nothing.css` | `{"error": "Page not found."}` | same |

The bytes are written out rather than handed to `c.JSON`, because Django's `JsonResponse` uses `json.dumps`' default separators and puts a space after the colon. The one remaining difference is the `charset=utf-8` gin appends to the content type, which every response in this service carries and Django's carry on neither.

### Why /static/ moved too

Django served it through whitenoise, out of `static-assets/collected-static`. Nothing in this repository asks for it: there is no Django admin in the URL conf, the DRF renderer list is `JSONRenderer` alone so there is no browsable API, and the web apps reference no `/static/` URL. The one thing that ever wanted it is drf-spectacular's Swagger UI, and `ENABLE_DRF_SPECTACULAR` defaults to `0` — its routes are not even in the route inventory. So in a default deployment every request here missed on the Django side too, and now misses with the same body. A deployment that turns drf-spectacular on loses `/api/schema/` and its assets, which is the one thing this line costs.

### Why migrator stays

Django owns the schema. Every table was created by a Django migration, Go never creates or alters one — `internal/manage` asks the database whether `django_migrations` holds every migration the Python app ships, and refuses to start until it does — and there is no `migrate` on the Go side to put in its place. `migrator` runs once and exits, so no Python process serves anything once it has.

## The schema, part one: recording what Django's migrations do

`migrator` is the last Python service, and this is the first half of taking it. The Django app ships 164 migrations — 122 in `db`, six in `license`, and 36 more from Django and django-celery-beat — and Go has to leave a database in exactly the state they leave it in.

None of them are rewritten here. Django is the only authority on what its own migrations do, and it will tell you: `internal/migrate/sql/<app>/<name>.sql` holds the statements a real `migrate` executed, captured through `connection.execute_wrapper` as they ran. The schema half of the port is therefore faithful by construction rather than by transcription, and CI regenerates all of it and diffs it, so a migration added to the Python app cannot be forgotten here.

### Why the statements are recorded and not rendered

`sqlmigrate` is the obvious tool and it is not good enough. It renders a migration without executing it, so an operation that asks the database the name of a constraint an earlier operation in the same migration was supposed to have created finds nothing there. `db.0074` drops a unique_together, alters the field and puts it back; rendering it raises `Found wrong number (0) of constraints` rather than printing anything.

Recording a real run has no such problem, and three things had to be got right to make it usable:

- **The filter is about reads, not first words.** Most of what a migration runs is Django reading the catalogue to decide what DDL to emit — 3140 of the first 5082 statements captured were `SELECT`s against `pg_class` and `pg_constraint`, and they are also the only statements carrying bound parameters. Skipping them is right. Skipping by first word was not: Django drops a foreign key with a compound statement beginning `SET CONSTRAINTS ... ; ALTER TABLE ... DROP CONSTRAINT`, so excluding anything starting with `SET` threw the drop away, and `db.0002` then tried to add a constraint that was still there.
- **Two tables' rows are not a migration's to write.** `django_content_type` and `auth_permission` are filled in by `post_migrate` handlers rather than by any operation. Their *tables* are created by ordinary migrations and are recorded like anything else; their contents are written out separately, as the end state rather than the incremental inserts. Nothing in Plane reads either one — there is no `GenericForeignKey` in the app, and its permission classes are DRF's rather than Django's — but a database this builds and a database Django builds have to be the same thing, or comparing them stops meaning anything.
- **`Migration.atomic` is carried in the plan.** Two migrations set it false, and they have to: `CREATE INDEX CONCURRENTLY` is refused inside a transaction block, so `db.0103` cannot be wrapped in one. Every other migration is one transaction — its statements, its code, and its ledger row together.

The ledger is Django's own `django_migrations`, unchanged, because both sides read it: this package to know what is applied, and `wait_for_migrations` to know whether anything is outstanding. Its table is created by Django's `MigrationRecorder` before the first migration runs and so belongs to none of them, which is why its definition is recorded into `ledger.sql` of its own.

### What is proven

`TestGoBuildsTheSameSchemaAsDjango` applies the whole plan to an empty database and compares the result against `testdata/schema.tsv`, which is the same query run against a database Django migrated. The query asks for every column, constraint, index and sequence, sorted — rather than `pg_dump`, whose output carries the order objects happen to sit in the catalogue.

All 2885 rows match. So do the other three things worth comparing:

| | Django | Go |
| --- | --- | --- |
| schema facts | 2885 | identical |
| tables | 110 | 110 |
| `django_migrations` rows | 164 | identical, same names |
| `django_content_type` rows | 121 | identical, same ids |
| `auth_permission` rows | 512 | identical, same ids |

And one result that was not expected: on a fresh database, **every `RunPython` operation is a no-op**. A Django-migrated empty database has rows in exactly three tables, all of them bookkeeping, and Go's has the same rows. The data migrations exist to move data that is already there, so a fresh install built by Go is already indistinguishable from one built by Django.

### What is not done

The command refuses anyway. 58 operations across 34 migrations carry Python, and until they are ported `manage migrate` names them and stops rather than applying anything.

That is deliberate. "It happens to be empty" is not something to bet a schema on, and the refusal is exactly what protects the case that matters: upgrading a database that has data in it. `migrator` stays on Python until those 58 are ported.

## The schema, part two: the operations that carry code

The recording covers what Django does to the shape of a database. It cannot cover the 58 `RunPython` operations, because what those do depends on the rows already there, and the database they were recorded against was empty. Those are ported by hand, and the question is how to know a port is right.

Not by reading. The places a `RunPython` port goes wrong are not the places it looks difficult, and two of the first six proved it:

- **`is None` catches two different nulls.** `update_workspace_member_props` branches on `obj.view_props is None`. Reading that, a `view_props IS NULL` looks exact — and it is wrong. A `jsonb` column holding the JSON value `null` comes back from psycopg as Python `None` just as a SQL `NULL` does, so the ORM cannot tell them apart and both take the first branch. Written the obvious way, a member whose `view_props` was JSON null came out with `"properties": null` where Django gives it the full defaults.
- **Two passes are not one pass.** The same operation written as "fill the nulls, then wrap the rest" rewrites rows the first pass had just written, and Postgres then refuses the `ALTER TABLE` that follows with `cannot ALTER TABLE because it has pending trigger events`. Django's `bulk_update` is a single `UPDATE` and does not provoke it. One `CASE` is both the closer reproduction and the one that works.

Neither was found by argument. Both were found by running the two implementations against the same rows.

### The check

`tools/check_migration_operations.py` builds two databases, migrates both to the migration *before* the one under test, seeds them identically from `internal/migrate/testdata/operations/<app>.<name>.sql`, then lets Django apply the migration to one and the Go engine apply it to the other. Every row of every table is dumped from both and compared. CI runs it.

Two things make that possible. `migrate.ApplyThrough` stops the Go engine at a named migration, which is also what `manage migrate <app> <name>` does and what Django's own command has always done. And the recorded files now say *where* a coded operation goes, with a `-- RUN app.name.function` line in the position Django ran the Python one — which is not a detail. `db.0035` adds `organization_size`, fills it in from `company_size`, and drops `company_size` in the same migration; replaying all the SQL and then all the code read a column that was no longer there.

Four of the operations exist to scatter rows into an arbitrary order and call `random.randint` to do it. Two runs of the same Python disagree with each other, so comparing those values would only prove that random is random. A seed names them with a `-- RANDOM: table.column` line: the column is left out of the comparison, and checked separately for having been filled in at all.

### Three upstream bugs, reproduced

Reading these closely turns up things that are plainly not what was meant, and they are reproduced rather than corrected, because a database that has been through the Python is in the state the Python left it in.

- **`update_cycle_props` and `update_module_props` test for a key that does not exist.** The condition is `if "filter" in obj.view_props`; the key the rewrite then reads, and the one every other props object in the codebase uses, is `filters`. Nothing writes a `filter`, so in practice these two rewrite nothing at all.
- **`generate_display_name`'s random fallback is unreachable.** It reads `obj.email.split("@")[0] if len(obj.email.split("@")) else "".join(random.choice(...) for _ in range(6))`. `str.split` always returns at least one element — `"".split("@")` is `[""]` — so the length is never zero and the six random letters are never generated.
- **`random_sort_ordering` uses a different range from its four siblings.** The other scatterings are `randint(1, 65536)`; this one is `randint(0, 65535)`.

### Masking, for an identifier that cannot match

`db.0049`'s `update_pages` writes a fresh `uuid4().hex` into the middle of a page's HTML as well as into a column of its own. Excluding the whole column would leave the operation's real output unchecked — which blocks were embedded, in what order, with what titles. A seed can instead say `-- MASK-HEX32: pages.description_html`, which blanks the thirty-two hex characters and leaves the rest of the markup in the comparison.

The same operation shows why the id has to be generated once and used twice: a materialised CTE holds the blocks, the aggregate builds the HTML from it and the insert writes the log rows from it. It is also written in two forms — Python hands the undashed hex to the f-string and the same string to a `UUIDField`, which parses it and stores the canonical dashed form.

Two operations wrap everything in `try: ... except Exception as e: print(e)`, and carry on as though nothing had been asked of them. In Postgres an error inside a transaction poisons the whole thing, so reproducing that needs a savepoint.

### Where it stands

26 operations across 12 migrations are ported and checked: `db.0035` through `db.0050`. Two more are registered as doing nothing, with the reason written down — `contenttypes.0002`'s is Django's own `RunPython.noop`, and `auth.0011` only touches the two tables whose contents come from the recorded end state.

That leaves 29. Until they are done `manage migrate` still refuses, and `migrator` still runs Python.
