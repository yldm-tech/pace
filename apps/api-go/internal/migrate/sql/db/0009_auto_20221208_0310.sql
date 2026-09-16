-- db.0009_auto_20221208_0310, recorded by apps/api-go/tools/generate_migration_sql.py. Do not edit by hand.
ALTER TABLE "project_member" ADD COLUMN "view_props" jsonb NULL;
ALTER TABLE "state" ADD COLUMN "group" varchar(20) DEFAULT 'backlog' NOT NULL;
ALTER TABLE "state" ALTER COLUMN "group" DROP DEFAULT;
