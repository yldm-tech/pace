-- db.0082_alter_issue_managers_alter_cycleissue_issue_and_more, recorded by apps/api-go/tools/generate_migration_sql.py. Do not edit by hand.
SET CONSTRAINTS "cycle_issue_issue_id_fd06e284_fk_issue_id" IMMEDIATE; ALTER TABLE "cycle_issues" DROP CONSTRAINT "cycle_issue_issue_id_fd06e284_fk_issue_id";
ALTER TABLE "cycle_issues" DROP CONSTRAINT "cycle_issue_issue_id_fd06e284_uniq";
CREATE INDEX "cycle_issues_issue_id_2d5ac97f" ON "cycle_issues" ("issue_id");
ALTER TABLE "cycle_issues" ADD CONSTRAINT "cycle_issues_issue_id_2d5ac97f_fk_issues_id" FOREIGN KEY ("issue_id") REFERENCES "issues" ("id") DEFERRABLE INITIALLY DEFERRED;
SET CONSTRAINTS "draft_issue_cycles_draft_issue_id_ed45e8a2_fk_draft_issues_id" IMMEDIATE; ALTER TABLE "draft_issue_cycles" DROP CONSTRAINT "draft_issue_cycles_draft_issue_id_ed45e8a2_fk_draft_issues_id";
ALTER TABLE "draft_issue_cycles" DROP CONSTRAINT "draft_issue_cycles_draft_issue_id_key";
CREATE INDEX "draft_issue_cycles_draft_issue_id_ed45e8a2" ON "draft_issue_cycles" ("draft_issue_id");
ALTER TABLE "draft_issue_cycles" ADD CONSTRAINT "draft_issue_cycles_draft_issue_id_ed45e8a2_fk_draft_issues_id" FOREIGN KEY ("draft_issue_id") REFERENCES "draft_issues" ("id") DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE "cycle_issues" ADD CONSTRAINT "cycle_issues_issue_id_cycle_id_deleted_at_93e8fecd_uniq" UNIQUE ("issue_id", "cycle_id", "deleted_at");
ALTER TABLE "draft_issue_cycles" ADD CONSTRAINT "draft_issue_cycles_draft_issue_id_cycle_id__e133e097_uniq" UNIQUE ("draft_issue_id", "cycle_id", "deleted_at");
CREATE UNIQUE INDEX "cycle_issue_when_deleted_at_null" ON "cycle_issues" ("cycle_id", "issue_id") WHERE "deleted_at" IS NULL;
CREATE UNIQUE INDEX "draft_issue_cycle_when_deleted_at_null" ON "draft_issue_cycles" ("draft_issue_id", "cycle_id") WHERE "deleted_at" IS NULL;
