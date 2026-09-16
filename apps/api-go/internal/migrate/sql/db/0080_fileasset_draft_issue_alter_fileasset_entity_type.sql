-- db.0080_fileasset_draft_issue_alter_fileasset_entity_type, recorded by apps/api-go/tools/generate_migration_sql.py. Do not edit by hand.
ALTER TABLE "file_assets" ADD COLUMN "draft_issue_id" uuid NULL CONSTRAINT "file_assets_draft_issue_id_52633145_fk_draft_issues_id" REFERENCES "draft_issues"("id") DEFERRABLE INITIALLY DEFERRED; SET CONSTRAINTS "file_assets_draft_issue_id_52633145_fk_draft_issues_id" IMMEDIATE;
CREATE INDEX "file_assets_draft_issue_id_52633145" ON "file_assets" ("draft_issue_id");
