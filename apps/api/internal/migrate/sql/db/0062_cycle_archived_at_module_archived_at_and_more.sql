-- db.0062_cycle_archived_at_module_archived_at_and_more, recorded from the Django app this replaced.
ALTER TABLE "cycles" ADD COLUMN "archived_at" timestamp with time zone NULL;
ALTER TABLE "modules" ADD COLUMN "archived_at" timestamp with time zone NULL;
ALTER TABLE "projects" ADD COLUMN "archived_at" timestamp with time zone NULL;
