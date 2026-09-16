-- db.0032_auto_20230520_2015, recorded by apps/api-go/tools/generate_migration_sql.py. Do not edit by hand.
ALTER TABLE "projects" RENAME COLUMN "icon" TO "emoji";
ALTER TABLE "projects" ADD COLUMN "icon_prop" jsonb NULL;
