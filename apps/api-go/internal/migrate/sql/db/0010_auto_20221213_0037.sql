-- db.0010_auto_20221213_0037, recorded from the Django app this replaced.
ALTER TABLE "project_identifier" ADD COLUMN "workspace_id" uuid NULL CONSTRAINT "project_identifier_workspace_id_6024b517_fk_workspace_id" REFERENCES "workspace"("id") DEFERRABLE INITIALLY DEFERRED; SET CONSTRAINTS "project_identifier_workspace_id_6024b517_fk_workspace_id" IMMEDIATE;
ALTER TABLE "project" ALTER COLUMN "identifier" SET NOT NULL;
ALTER TABLE "project" ADD CONSTRAINT "project_identifier_workspace_id_35e9a4bf_uniq" UNIQUE ("identifier", "workspace_id");
ALTER TABLE "project_identifier" ADD CONSTRAINT "project_identifier_name_workspace_id_4b7404cb_uniq" UNIQUE ("name", "workspace_id");
CREATE INDEX "project_identifier_workspace_id_6024b517" ON "project_identifier" ("workspace_id");
