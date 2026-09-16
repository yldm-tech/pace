-- db.0062_cycle_archived_at_module_archived_at_and_more, recorded by apps/api-go/tools/generate_migration_sql.py. Do not edit by hand.
ALTER TABLE "cycles" ADD COLUMN "archived_at" timestamp with time zone NULL;
ALTER TABLE "modules" ADD COLUMN "archived_at" timestamp with time zone NULL;
ALTER TABLE "projects" ADD COLUMN "archived_at" timestamp with time zone NULL;
