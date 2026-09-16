-- db.0051_cycle_external_id_cycle_external_source_and_more, recorded by apps/api-go/tools/generate_migration_sql.py. Do not edit by hand.
ALTER TABLE "cycles" ADD COLUMN "external_id" varchar(255) NULL;
ALTER TABLE "cycles" ADD COLUMN "external_source" varchar(255) NULL;
ALTER TABLE "inbox_issues" ADD COLUMN "external_id" varchar(255) NULL;
ALTER TABLE "inbox_issues" ADD COLUMN "external_source" varchar(255) NULL;
ALTER TABLE "issues" ADD COLUMN "external_id" varchar(255) NULL;
ALTER TABLE "issues" ADD COLUMN "external_source" varchar(255) NULL;
ALTER TABLE "issue_comments" ADD COLUMN "external_id" varchar(255) NULL;
ALTER TABLE "issue_comments" ADD COLUMN "external_source" varchar(255) NULL;
ALTER TABLE "labels" ADD COLUMN "external_id" varchar(255) NULL;
ALTER TABLE "labels" ADD COLUMN "external_source" varchar(255) NULL;
ALTER TABLE "modules" ADD COLUMN "external_id" varchar(255) NULL;
ALTER TABLE "modules" ADD COLUMN "external_source" varchar(255) NULL;
ALTER TABLE "states" ADD COLUMN "external_id" varchar(255) NULL;
ALTER TABLE "states" ADD COLUMN "external_source" varchar(255) NULL;
