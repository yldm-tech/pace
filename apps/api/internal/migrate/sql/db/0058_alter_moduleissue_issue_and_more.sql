-- db.0058_alter_moduleissue_issue_and_more, recorded from the Django app this replaced.
SET CONSTRAINTS "module_issues_issue_id_7caa908b_fk_issue_id" IMMEDIATE; ALTER TABLE "module_issues" DROP CONSTRAINT "module_issues_issue_id_7caa908b_fk_issue_id";
ALTER TABLE "module_issues" DROP CONSTRAINT "module_issues_issue_id_7caa908b_uniq";
CREATE INDEX "module_issues_issue_id_7caa908b" ON "module_issues" ("issue_id");
ALTER TABLE "module_issues" ADD CONSTRAINT "module_issues_issue_id_7caa908b_fk_issues_id" FOREIGN KEY ("issue_id") REFERENCES "issues" ("id") DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE "module_issues" ADD CONSTRAINT "module_issues_issue_id_module_id_d0042711_uniq" UNIQUE ("issue_id", "module_id");
