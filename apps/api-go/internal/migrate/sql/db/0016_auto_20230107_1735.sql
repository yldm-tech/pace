-- db.0016_auto_20230107_1735, recorded by apps/api-go/tools/generate_migration_sql.py. Do not edit by hand.
ALTER TABLE "file_assets" ADD COLUMN "workspace_id" uuid NULL CONSTRAINT "file_assets_workspace_id_fa50b9c5_fk_workspaces_id" REFERENCES "workspaces"("id") DEFERRABLE INITIALLY DEFERRED; SET CONSTRAINTS "file_assets_workspace_id_fa50b9c5_fk_workspaces_id" IMMEDIATE;
CREATE INDEX "file_assets_workspace_id_fa50b9c5" ON "file_assets" ("workspace_id");
