-- db.0088_sticky_sort_order_workspaceuserlink, recorded from the Django app this replaced.
ALTER TABLE "stickies" ADD COLUMN "sort_order" double precision DEFAULT 65535.0 NOT NULL;
ALTER TABLE "stickies" ALTER COLUMN "sort_order" DROP DEFAULT;
CREATE TABLE "workspace_user_links" ("created_at" timestamp with time zone NOT NULL, "updated_at" timestamp with time zone NOT NULL, "deleted_at" timestamp with time zone NULL, "id" uuid NOT NULL PRIMARY KEY, "title" varchar(255) NULL, "url" text NOT NULL, "metadata" jsonb NOT NULL, "created_by_id" uuid NULL, "owner_id" uuid NOT NULL, "project_id" uuid NULL, "updated_by_id" uuid NULL, "workspace_id" uuid NOT NULL);
ALTER TABLE "webhooks" DROP CONSTRAINT "webhooks_workspace_id_url_9e8012ba_uniq";
ALTER TABLE "webhooks" ADD CONSTRAINT "webhooks_workspace_id_url_deleted_at_ea7a1429_uniq" UNIQUE ("workspace_id", "url", "deleted_at");
CREATE UNIQUE INDEX "webhook_url_unique_url_when_deleted_at_null" ON "webhooks" ("workspace_id", "url") WHERE "deleted_at" IS NULL;
ALTER TABLE "workspace_user_links" ADD CONSTRAINT "workspace_user_links_created_by_id_b9ce7a5d_fk_users_id" FOREIGN KEY ("created_by_id") REFERENCES "users" ("id") DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE "workspace_user_links" ADD CONSTRAINT "workspace_user_links_owner_id_37d99444_fk_users_id" FOREIGN KEY ("owner_id") REFERENCES "users" ("id") DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE "workspace_user_links" ADD CONSTRAINT "workspace_user_links_project_id_045e0d53_fk_projects_id" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE "workspace_user_links" ADD CONSTRAINT "workspace_user_links_updated_by_id_bd0b017f_fk_users_id" FOREIGN KEY ("updated_by_id") REFERENCES "users" ("id") DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE "workspace_user_links" ADD CONSTRAINT "workspace_user_links_workspace_id_1b0a8e22_fk_workspaces_id" FOREIGN KEY ("workspace_id") REFERENCES "workspaces" ("id") DEFERRABLE INITIALLY DEFERRED;
CREATE INDEX "workspace_user_links_created_by_id_b9ce7a5d" ON "workspace_user_links" ("created_by_id");
CREATE INDEX "workspace_user_links_owner_id_37d99444" ON "workspace_user_links" ("owner_id");
CREATE INDEX "workspace_user_links_project_id_045e0d53" ON "workspace_user_links" ("project_id");
CREATE INDEX "workspace_user_links_updated_by_id_bd0b017f" ON "workspace_user_links" ("updated_by_id");
CREATE INDEX "workspace_user_links_workspace_id_1b0a8e22" ON "workspace_user_links" ("workspace_id");
