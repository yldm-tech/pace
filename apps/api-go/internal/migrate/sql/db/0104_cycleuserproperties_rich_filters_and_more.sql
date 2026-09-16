-- db.0104_cycleuserproperties_rich_filters_and_more, recorded by apps/api-go/tools/generate_migration_sql.py. Do not edit by hand.
ALTER TABLE "cycle_user_properties" ADD COLUMN "rich_filters" jsonb DEFAULT '{}'::jsonb NOT NULL;
ALTER TABLE "cycle_user_properties" ALTER COLUMN "rich_filters" DROP DEFAULT;
ALTER TABLE "exporters" ADD COLUMN "rich_filters" jsonb DEFAULT '{}'::jsonb NULL;
ALTER TABLE "exporters" ALTER COLUMN "rich_filters" DROP DEFAULT;
ALTER TABLE "issue_user_properties" ADD COLUMN "rich_filters" jsonb DEFAULT '{}'::jsonb NOT NULL;
ALTER TABLE "issue_user_properties" ALTER COLUMN "rich_filters" DROP DEFAULT;
ALTER TABLE "issue_views" ADD COLUMN "rich_filters" jsonb DEFAULT '{}'::jsonb NOT NULL;
ALTER TABLE "issue_views" ALTER COLUMN "rich_filters" DROP DEFAULT;
ALTER TABLE "module_user_properties" ADD COLUMN "rich_filters" jsonb DEFAULT '{}'::jsonb NOT NULL;
ALTER TABLE "module_user_properties" ALTER COLUMN "rich_filters" DROP DEFAULT;
ALTER TABLE "workspace_user_properties" ADD COLUMN "rich_filters" jsonb DEFAULT '{}'::jsonb NOT NULL;
ALTER TABLE "workspace_user_properties" ALTER COLUMN "rich_filters" DROP DEFAULT;
