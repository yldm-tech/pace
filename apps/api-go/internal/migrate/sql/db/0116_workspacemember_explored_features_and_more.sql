-- db.0116_workspacemember_explored_features_and_more, recorded by apps/api-go/tools/generate_migration_sql.py. Do not edit by hand.
ALTER TABLE "profiles" ADD COLUMN "notification_view_mode" varchar(255) DEFAULT 'full' NOT NULL;
ALTER TABLE "profiles" ALTER COLUMN "notification_view_mode" DROP DEFAULT;
ALTER TABLE "users" ADD COLUMN "is_password_reset_required" boolean DEFAULT false NOT NULL;
ALTER TABLE "users" ALTER COLUMN "is_password_reset_required" DROP DEFAULT;
ALTER TABLE "workspace_members" ADD COLUMN "explored_features" jsonb DEFAULT '{}'::jsonb NOT NULL;
ALTER TABLE "workspace_members" ALTER COLUMN "explored_features" DROP DEFAULT;
ALTER TABLE "workspace_members" ADD COLUMN "getting_started_checklist" jsonb DEFAULT '{}'::jsonb NOT NULL;
ALTER TABLE "workspace_members" ALTER COLUMN "getting_started_checklist" DROP DEFAULT;
ALTER TABLE "workspace_members" ADD COLUMN "tips" jsonb DEFAULT '{}'::jsonb NOT NULL;
ALTER TABLE "workspace_members" ALTER COLUMN "tips" DROP DEFAULT;
