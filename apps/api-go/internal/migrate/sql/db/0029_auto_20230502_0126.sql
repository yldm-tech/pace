-- db.0029_auto_20230502_0126, recorded by apps/api-go/tools/generate_migration_sql.py. Do not edit by hand.
ALTER TABLE "cycles" ADD COLUMN "view_props" jsonb DEFAULT '{}'::jsonb NOT NULL;
ALTER TABLE "cycles" ALTER COLUMN "view_props" DROP DEFAULT;
ALTER TABLE "importers" ADD COLUMN "imported_data" jsonb NULL;
ALTER TABLE "modules" ADD COLUMN "view_props" jsonb DEFAULT '{}'::jsonb NOT NULL;
ALTER TABLE "modules" ALTER COLUMN "view_props" DROP DEFAULT;
CREATE TABLE "slack_project_syncs" ("created_at" timestamp with time zone NOT NULL, "updated_at" timestamp with time zone NOT NULL, "id" uuid NOT NULL PRIMARY KEY, "access_token" varchar(300) NOT NULL, "scopes" text NOT NULL, "bot_user_id" varchar(50) NOT NULL, "webhook_url" varchar(1000) NOT NULL, "data" jsonb NOT NULL, "team_id" varchar(30) NOT NULL, "team_name" varchar(300) NOT NULL, "created_by_id" uuid NULL, "project_id" uuid NOT NULL, "updated_by_id" uuid NULL, "workspace_id" uuid NOT NULL, "workspace_integration_id" uuid NOT NULL);
ALTER TABLE "slack_project_syncs" ADD CONSTRAINT "slack_project_syncs_team_id_project_id_50a144a7_uniq" UNIQUE ("team_id", "project_id");
ALTER TABLE "slack_project_syncs" ADD CONSTRAINT "slack_project_syncs_created_by_id_ec405a17_fk_users_id" FOREIGN KEY ("created_by_id") REFERENCES "users" ("id") DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE "slack_project_syncs" ADD CONSTRAINT "slack_project_syncs_project_id_016dc792_fk_projects_id" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE "slack_project_syncs" ADD CONSTRAINT "slack_project_syncs_updated_by_id_152eb3b5_fk_users_id" FOREIGN KEY ("updated_by_id") REFERENCES "users" ("id") DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE "slack_project_syncs" ADD CONSTRAINT "slack_project_syncs_workspace_id_d1822b06_fk_workspaces_id" FOREIGN KEY ("workspace_id") REFERENCES "workspaces" ("id") DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE "slack_project_syncs" ADD CONSTRAINT "slack_project_syncs_workspace_integratio_d89c9b40_fk_workspace" FOREIGN KEY ("workspace_integration_id") REFERENCES "workspace_integrations" ("id") DEFERRABLE INITIALLY DEFERRED;
CREATE INDEX "slack_project_syncs_created_by_id_ec405a17" ON "slack_project_syncs" ("created_by_id");
CREATE INDEX "slack_project_syncs_project_id_016dc792" ON "slack_project_syncs" ("project_id");
CREATE INDEX "slack_project_syncs_updated_by_id_152eb3b5" ON "slack_project_syncs" ("updated_by_id");
CREATE INDEX "slack_project_syncs_workspace_id_d1822b06" ON "slack_project_syncs" ("workspace_id");
CREATE INDEX "slack_project_syncs_workspace_integration_id_d89c9b40" ON "slack_project_syncs" ("workspace_integration_id");
