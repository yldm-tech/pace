-- db.0076_alter_projectmember_role_and_more, recorded by apps/api-go/tools/generate_migration_sql.py. Do not edit by hand.
ALTER TABLE "projects" ADD COLUMN "guest_view_all_features" boolean DEFAULT false NOT NULL;
ALTER TABLE "projects" ALTER COLUMN "guest_view_all_features" DROP DEFAULT;
-- RUN db.0076_alter_projectmember_role_and_more.update_workspace_project_member_role
