-- db.0046_label_sort_order_alter_analyticview_created_by_and_more, recorded by apps/api-go/tools/generate_migration_sql.py. Do not edit by hand.
ALTER TABLE "labels" ADD COLUMN "sort_order" double precision DEFAULT 65535.0 NOT NULL;
ALTER TABLE "labels" ALTER COLUMN "sort_order" DROP DEFAULT;
CREATE TABLE "issue_mentions" ("created_at" timestamp with time zone NOT NULL, "updated_at" timestamp with time zone NOT NULL, "id" uuid NOT NULL PRIMARY KEY, "created_by_id" uuid NULL, "issue_id" uuid NOT NULL, "mention_id" uuid NOT NULL, "project_id" uuid NOT NULL, "updated_by_id" uuid NULL, "workspace_id" uuid NOT NULL);
ALTER TABLE "issue_mentions" ADD CONSTRAINT "issue_mentions_issue_id_mention_id_ce91a005_uniq" UNIQUE ("issue_id", "mention_id");
ALTER TABLE "issue_mentions" ADD CONSTRAINT "issue_mentions_created_by_id_eb44759e_fk_users_id" FOREIGN KEY ("created_by_id") REFERENCES "users" ("id") DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE "issue_mentions" ADD CONSTRAINT "issue_mentions_issue_id_d8821107_fk_issues_id" FOREIGN KEY ("issue_id") REFERENCES "issues" ("id") DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE "issue_mentions" ADD CONSTRAINT "issue_mentions_mention_id_cf1b9346_fk_users_id" FOREIGN KEY ("mention_id") REFERENCES "users" ("id") DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE "issue_mentions" ADD CONSTRAINT "issue_mentions_project_id_d0cccdf5_fk_projects_id" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE "issue_mentions" ADD CONSTRAINT "issue_mentions_updated_by_id_c62106d3_fk_users_id" FOREIGN KEY ("updated_by_id") REFERENCES "users" ("id") DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE "issue_mentions" ADD CONSTRAINT "issue_mentions_workspace_id_4ca59d05_fk_workspaces_id" FOREIGN KEY ("workspace_id") REFERENCES "workspaces" ("id") DEFERRABLE INITIALLY DEFERRED;
CREATE INDEX "issue_mentions_created_by_id_eb44759e" ON "issue_mentions" ("created_by_id");
CREATE INDEX "issue_mentions_issue_id_d8821107" ON "issue_mentions" ("issue_id");
CREATE INDEX "issue_mentions_mention_id_cf1b9346" ON "issue_mentions" ("mention_id");
CREATE INDEX "issue_mentions_project_id_d0cccdf5" ON "issue_mentions" ("project_id");
CREATE INDEX "issue_mentions_updated_by_id_c62106d3" ON "issue_mentions" ("updated_by_id");
CREATE INDEX "issue_mentions_workspace_id_4ca59d05" ON "issue_mentions" ("workspace_id");
