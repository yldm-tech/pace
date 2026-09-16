-- db.0050_user_use_case_alter_workspace_organization_size, recorded by apps/api-go/tools/generate_migration_sql.py. Do not edit by hand.
ALTER TABLE "users" ADD COLUMN "use_case" text NULL;
ALTER TABLE "workspaces" ALTER COLUMN "organization_size" DROP NOT NULL;
ALTER TABLE "file_assets" ADD COLUMN "is_deleted" boolean DEFAULT false NOT NULL;
ALTER TABLE "file_assets" ALTER COLUMN "is_deleted" DROP DEFAULT;
