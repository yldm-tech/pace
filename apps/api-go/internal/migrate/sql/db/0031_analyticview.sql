-- db.0031_analyticview, recorded from the Django app this replaced.
CREATE TABLE "analytic_views" ("created_at" timestamp with time zone NOT NULL, "updated_at" timestamp with time zone NOT NULL, "id" uuid NOT NULL PRIMARY KEY, "name" varchar(255) NOT NULL, "description" text NOT NULL, "query" jsonb NOT NULL, "query_dict" jsonb NOT NULL, "created_by_id" uuid NULL, "updated_by_id" uuid NULL, "workspace_id" uuid NOT NULL);
ALTER TABLE "analytic_views" ADD CONSTRAINT "analytic_views_created_by_id_1b3ca0a9_fk_users_id" FOREIGN KEY ("created_by_id") REFERENCES "users" ("id") DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE "analytic_views" ADD CONSTRAINT "analytic_views_updated_by_id_b6d827e1_fk_users_id" FOREIGN KEY ("updated_by_id") REFERENCES "users" ("id") DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE "analytic_views" ADD CONSTRAINT "analytic_views_workspace_id_ca6e5c0b_fk_workspaces_id" FOREIGN KEY ("workspace_id") REFERENCES "workspaces" ("id") DEFERRABLE INITIALLY DEFERRED;
CREATE INDEX "analytic_views_created_by_id_1b3ca0a9" ON "analytic_views" ("created_by_id");
CREATE INDEX "analytic_views_updated_by_id_b6d827e1" ON "analytic_views" ("updated_by_id");
CREATE INDEX "analytic_views_workspace_id_ca6e5c0b" ON "analytic_views" ("workspace_id");
