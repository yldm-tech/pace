-- db.0013_auto_20230107_0041, recorded by apps/api-go/tools/generate_migration_sql.py. Do not edit by hand.
ALTER TABLE "issues" ADD COLUMN "description_html" text DEFAULT '' NOT NULL;
ALTER TABLE "issues" ALTER COLUMN "description_html" DROP DEFAULT;
ALTER TABLE "issues" ADD COLUMN "description_stripped" text DEFAULT '' NOT NULL;
ALTER TABLE "issues" ALTER COLUMN "description_stripped" DROP DEFAULT;
ALTER TABLE "users" ADD COLUMN "role" varchar(300) NULL;
ALTER TABLE "workspace_members" ADD COLUMN "view_props" jsonb NULL;
