-- db.0085_intake_intakeissue_remove_inboxissue_created_by_and_more, recorded by apps/api-go/tools/generate_migration_sql.py. Do not edit by hand.
ALTER TABLE "inboxes" RENAME TO "intakes";
SET CONSTRAINTS "inbox_issues_inbox_id_444b05b9_fk_inboxes_id" IMMEDIATE; ALTER TABLE "inbox_issues" DROP CONSTRAINT "inbox_issues_inbox_id_444b05b9_fk_inboxes_id";
ALTER TABLE "inbox_issues" RENAME COLUMN "inbox_id" TO "intake_id";
ALTER TABLE "inbox_issues" ADD CONSTRAINT "inbox_issues_intake_id_a04a7455_fk_intakes_id" FOREIGN KEY ("intake_id") REFERENCES "intakes" ("id") DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE "inbox_issues" RENAME TO "intake_issues";
ALTER TABLE "projects" RENAME COLUMN "inbox_view" TO "intake_view";
SET CONSTRAINTS "deploy_boards_inbox_id_ebc13d44_fk_inboxes_id" IMMEDIATE; ALTER TABLE "deploy_boards" DROP CONSTRAINT "deploy_boards_inbox_id_ebc13d44_fk_inboxes_id";
ALTER TABLE "deploy_boards" RENAME COLUMN "inbox_id" TO "intake_id";
ALTER TABLE "deploy_boards" ADD CONSTRAINT "deploy_boards_intake_id_76a6470a_fk_intakes_id" FOREIGN KEY ("intake_id") REFERENCES "intakes" ("id") DEFERRABLE INITIALLY DEFERRED;
SET CONSTRAINTS "project_deploy_boards_inbox_id_a6a75525_fk_inboxes_id" IMMEDIATE; ALTER TABLE "project_deploy_boards" DROP CONSTRAINT "project_deploy_boards_inbox_id_a6a75525_fk_inboxes_id";
ALTER TABLE "project_deploy_boards" RENAME COLUMN "inbox_id" TO "intake_id";
ALTER TABLE "project_deploy_boards" ADD CONSTRAINT "project_deploy_boards_intake_id_36aa612d_fk_intakes_id" FOREIGN KEY ("intake_id") REFERENCES "intakes" ("id") DEFERRABLE INITIALLY DEFERRED;
DROP INDEX IF EXISTS "inbox_unique_name_project_when_deleted_at_null";
CREATE UNIQUE INDEX "intake_unique_name_project_when_deleted_at_null" ON "intakes" ("name", "project_id") WHERE "deleted_at" IS NULL;
