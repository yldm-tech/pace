-- db.0106_auto_20250912_0845, recorded from the Django app this replaced.
CREATE TABLE "project_webhooks" ("created_at" timestamp with time zone NOT NULL, "updated_at" timestamp with time zone NOT NULL, "deleted_at" timestamp with time zone NULL, "id" uuid NOT NULL PRIMARY KEY, "created_by_id" uuid NULL, "project_id" uuid NOT NULL, "updated_by_id" uuid NULL, "webhook_id" uuid NOT NULL, "workspace_id" uuid NOT NULL);
CREATE UNIQUE INDEX "project_webhook_unique_project_webhook_when_deleted_at_null" ON "project_webhooks" ("project_id", "webhook_id") WHERE "deleted_at" IS NULL;
ALTER TABLE "project_webhooks" ADD CONSTRAINT "project_webhooks_project_id_webhook_id_deleted_at_dcfdb35d_uniq" UNIQUE ("project_id", "webhook_id", "deleted_at");
-- RUN db.0106_auto_20250912_0845.set_page_sort_order
ALTER TABLE "project_webhooks" ADD CONSTRAINT "project_webhooks_created_by_id_c3e4bfa3_fk_users_id" FOREIGN KEY ("created_by_id") REFERENCES "users" ("id") DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE "project_webhooks" ADD CONSTRAINT "project_webhooks_project_id_bec3cf8c_fk_projects_id" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE "project_webhooks" ADD CONSTRAINT "project_webhooks_updated_by_id_a0183aeb_fk_users_id" FOREIGN KEY ("updated_by_id") REFERENCES "users" ("id") DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE "project_webhooks" ADD CONSTRAINT "project_webhooks_webhook_id_da27c6a7_fk_webhooks_id" FOREIGN KEY ("webhook_id") REFERENCES "webhooks" ("id") DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE "project_webhooks" ADD CONSTRAINT "project_webhooks_workspace_id_429ebf05_fk_workspaces_id" FOREIGN KEY ("workspace_id") REFERENCES "workspaces" ("id") DEFERRABLE INITIALLY DEFERRED;
CREATE INDEX "project_webhooks_created_by_id_c3e4bfa3" ON "project_webhooks" ("created_by_id");
CREATE INDEX "project_webhooks_project_id_bec3cf8c" ON "project_webhooks" ("project_id");
CREATE INDEX "project_webhooks_updated_by_id_a0183aeb" ON "project_webhooks" ("updated_by_id");
CREATE INDEX "project_webhooks_webhook_id_da27c6a7" ON "project_webhooks" ("webhook_id");
CREATE INDEX "project_webhooks_workspace_id_429ebf05" ON "project_webhooks" ("workspace_id");
