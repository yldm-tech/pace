-- db.0123_notification_notif_receiver_unread_idx, written by hand rather than recorded: the Django app the earlier files came from is gone, and this is the first migration added from here.
--
-- The index the unread badge counts through. Its predicate is that endpoint's WHERE exactly, so the index holds only the notifications still waiting on somebody and a person with years of read ones is never scanned over them. The two columns are the pair the count binds: the receiver, and the workspace the slug resolves to.
--
-- It is concurrent, which is why the plan marks this migration non-atomic — Postgres refuses CREATE INDEX CONCURRENTLY inside a transaction block, and building it any other way would lock writes to notifications for as long as the build takes. The cost of that is what a non-atomic migration always costs: a run that fails part way leaves an index behind marked invalid and no ledger row, so the index has to be dropped before the migration is run again.
CREATE INDEX CONCURRENTLY "notif_receiver_unread_idx" ON "notifications" ("receiver_id", "workspace_id") WHERE ("deleted_at" IS NULL AND "read_at" IS NULL AND "archived_at" IS NULL AND "snoozed_till" IS NULL);
