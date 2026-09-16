-- db.0114_projectuserproperty_delete_issueuserproperty_and_more, recorded by apps/api-go/tools/generate_migration_sql.py. Do not edit by hand.
ALTER TABLE "issue_user_properties" RENAME TO "project_user_properties";
ALTER TABLE "project_user_properties" ADD COLUMN "preferences" jsonb DEFAULT '{"pages": {"block_display": true}, "navigation": {"default_tab": "work_items", "hide_in_more_menu": []}}'::jsonb NOT NULL;
ALTER TABLE "project_user_properties" ALTER COLUMN "preferences" DROP DEFAULT;
ALTER TABLE "project_user_properties" ADD COLUMN "sort_order" double precision DEFAULT 65535.0 NOT NULL;
ALTER TABLE "project_user_properties" ALTER COLUMN "sort_order" DROP DEFAULT;
DROP INDEX IF EXISTS "issue_user_property_unique_user_project_when_deleted_at_null";
CREATE UNIQUE INDEX "project_user_property_unique_user_project_when_deleted_at_null" ON "project_user_properties" ("user_id", "project_id") WHERE "deleted_at" IS NULL;
