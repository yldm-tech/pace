-- db.0117_rename_description_draftissue_description_json_and_more, recorded by apps/api-go/tools/generate_migration_sql.py. Do not edit by hand.
ALTER TABLE "draft_issues" RENAME COLUMN "description" TO "description_json";
ALTER TABLE "issues" RENAME COLUMN "description" TO "description_json";
ALTER TABLE "pages" RENAME COLUMN "description" TO "description_json";
