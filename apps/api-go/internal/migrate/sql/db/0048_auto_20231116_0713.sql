-- db.0048_auto_20231116_0713, recorded by apps/api-go/tools/generate_migration_sql.py. Do not edit by hand.
CREATE TABLE "page_logs" ("created_at" timestamp with time zone NOT NULL, "updated_at" timestamp with time zone NOT NULL, "id" uuid NOT NULL PRIMARY KEY, "transaction" uuid NOT NULL, "entity_identifier" uuid NULL, "entity_name" varchar(30) NOT NULL, "created_by_id" uuid NULL, "page_id" uuid NOT NULL, "project_id" uuid NOT NULL, "updated_by_id" uuid NULL, "workspace_id" uuid NOT NULL);
ALTER TABLE "pages" ADD COLUMN "archived_at" date NULL;
ALTER TABLE "pages" ADD COLUMN "is_locked" boolean DEFAULT false NOT NULL;
ALTER TABLE "pages" ALTER COLUMN "is_locked" DROP DEFAULT;
ALTER TABLE "pages" ADD COLUMN "parent_id" uuid NULL CONSTRAINT "pages_parent_id_8b823409_fk_pages_id" REFERENCES "pages"("id") DEFERRABLE INITIALLY DEFERRED; SET CONSTRAINTS "pages_parent_id_8b823409_fk_pages_id" IMMEDIATE;
ALTER TABLE "page_logs" ADD CONSTRAINT "page_logs_page_id_transaction_9ab05334_uniq" UNIQUE ("page_id", "transaction");
ALTER TABLE "page_logs" ADD CONSTRAINT "page_logs_created_by_id_4a295aec_fk_users_id" FOREIGN KEY ("created_by_id") REFERENCES "users" ("id") DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE "page_logs" ADD CONSTRAINT "page_logs_page_id_0e0d747d_fk_pages_id" FOREIGN KEY ("page_id") REFERENCES "pages" ("id") DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE "page_logs" ADD CONSTRAINT "page_logs_project_id_d5117f7a_fk_projects_id" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE "page_logs" ADD CONSTRAINT "page_logs_updated_by_id_1995190b_fk_users_id" FOREIGN KEY ("updated_by_id") REFERENCES "users" ("id") DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE "page_logs" ADD CONSTRAINT "page_logs_workspace_id_be7bde64_fk_workspaces_id" FOREIGN KEY ("workspace_id") REFERENCES "workspaces" ("id") DEFERRABLE INITIALLY DEFERRED;
CREATE INDEX "page_logs_created_by_id_4a295aec" ON "page_logs" ("created_by_id");
CREATE INDEX "page_logs_page_id_0e0d747d" ON "page_logs" ("page_id");
CREATE INDEX "page_logs_project_id_d5117f7a" ON "page_logs" ("project_id");
CREATE INDEX "page_logs_updated_by_id_1995190b" ON "page_logs" ("updated_by_id");
CREATE INDEX "page_logs_workspace_id_be7bde64" ON "page_logs" ("workspace_id");
CREATE INDEX "pages_parent_id_8b823409" ON "pages" ("parent_id");
