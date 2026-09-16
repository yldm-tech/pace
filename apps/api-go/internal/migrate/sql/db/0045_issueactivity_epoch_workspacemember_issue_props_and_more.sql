-- db.0045_issueactivity_epoch_workspacemember_issue_props_and_more, recorded by apps/api-go/tools/generate_migration_sql.py. Do not edit by hand.
CREATE TABLE "global_views" ("created_at" timestamp with time zone NOT NULL, "updated_at" timestamp with time zone NOT NULL, "id" uuid NOT NULL PRIMARY KEY, "name" varchar(255) NOT NULL, "description" text NOT NULL, "query" jsonb NOT NULL, "access" smallint NOT NULL CHECK ("access" >= 0), "query_data" jsonb NOT NULL, "sort_order" double precision NOT NULL, "created_by_id" uuid NULL, "updated_by_id" uuid NULL, "workspace_id" uuid NOT NULL);
ALTER TABLE "workspace_members" ADD COLUMN "issue_props" jsonb DEFAULT '{"subscribed": true, "assigned": true, "created": true, "all_issues": true}'::jsonb NOT NULL;
ALTER TABLE "workspace_members" ALTER COLUMN "issue_props" DROP DEFAULT;
ALTER TABLE "issue_activities" ADD COLUMN "epoch" double precision NULL;
-- RUN db.0045_issueactivity_epoch_workspacemember_issue_props_and_more.update_issue_activity_priority
-- RUN db.0045_issueactivity_epoch_workspacemember_issue_props_and_more.update_issue_activity_blocked
ALTER TABLE "global_views" ADD CONSTRAINT "global_views_created_by_id_14b7d95c_fk_users_id" FOREIGN KEY ("created_by_id") REFERENCES "users" ("id") DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE "global_views" ADD CONSTRAINT "global_views_updated_by_id_112e0281_fk_users_id" FOREIGN KEY ("updated_by_id") REFERENCES "users" ("id") DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE "global_views" ADD CONSTRAINT "global_views_workspace_id_3c68eca7_fk_workspaces_id" FOREIGN KEY ("workspace_id") REFERENCES "workspaces" ("id") DEFERRABLE INITIALLY DEFERRED;
CREATE INDEX "global_views_created_by_id_14b7d95c" ON "global_views" ("created_by_id");
CREATE INDEX "global_views_updated_by_id_112e0281" ON "global_views" ("updated_by_id");
CREATE INDEX "global_views_workspace_id_3c68eca7" ON "global_views" ("workspace_id");
