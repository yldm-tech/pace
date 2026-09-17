-- db.0050_user_use_case_alter_workspace_organization_size, recorded from the Django app this replaced.
ALTER TABLE "users" ADD COLUMN "use_case" text NULL;
ALTER TABLE "workspaces" ALTER COLUMN "organization_size" DROP NOT NULL;
ALTER TABLE "file_assets" ADD COLUMN "is_deleted" boolean DEFAULT false NOT NULL;
ALTER TABLE "file_assets" ALTER COLUMN "is_deleted" DROP DEFAULT;
-- RUN db.0050_user_use_case_alter_workspace_organization_size.user_password_autoset
