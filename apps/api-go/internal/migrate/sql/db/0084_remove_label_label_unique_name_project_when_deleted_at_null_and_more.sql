-- db.0084_remove_label_label_unique_name_project_when_deleted_at_null_and_more, recorded from the Django app this replaced.
DROP INDEX IF EXISTS "label_unique_name_project_when_deleted_at_null";
ALTER TABLE "labels" DROP CONSTRAINT "labels_name_project_id_deleted_at_eebc553a_uniq";
ALTER TABLE "deploy_boards" ADD COLUMN "is_disabled" boolean DEFAULT false NOT NULL;
ALTER TABLE "deploy_boards" ALTER COLUMN "is_disabled" DROP DEFAULT;
ALTER TABLE "inbox_issues" ADD COLUMN "extra" jsonb DEFAULT '{}'::jsonb NOT NULL;
ALTER TABLE "inbox_issues" ALTER COLUMN "extra" DROP DEFAULT;
ALTER TABLE "inbox_issues" ADD COLUMN "source_email" text NULL;
ALTER TABLE "users" ADD COLUMN "bot_type" varchar(30) NULL;
ALTER TABLE "deploy_boards" ALTER COLUMN "entity_name" DROP NOT NULL;
ALTER TABLE "inbox_issues" ALTER COLUMN "source" TYPE varchar(255) USING "source"::varchar(255);
CREATE UNIQUE INDEX "unique_name_when_project_null_and_not_deleted" ON "labels" ("name") WHERE ("deleted_at" IS NULL AND "project_id" IS NULL);
CREATE UNIQUE INDEX "unique_project_name_when_not_deleted" ON "labels" ("project_id", "name") WHERE ("deleted_at" IS NULL AND "project_id" IS NOT NULL);
