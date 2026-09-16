-- db.0110_workspaceuserproperties_navigation_control_preference_and_more, recorded by apps/api-go/tools/generate_migration_sql.py. Do not edit by hand.
ALTER TABLE "workspace_user_properties" ADD COLUMN "navigation_control_preference" varchar(25) DEFAULT 'ACCORDION' NOT NULL;
ALTER TABLE "workspace_user_properties" ALTER COLUMN "navigation_control_preference" DROP DEFAULT;
ALTER TABLE "workspace_user_properties" ADD COLUMN "navigation_project_limit" integer DEFAULT 10 NOT NULL;
ALTER TABLE "workspace_user_properties" ALTER COLUMN "navigation_project_limit" DROP DEFAULT;
