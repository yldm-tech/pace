-- db.0041_cycle_sort_order_issuecomment_access_and_more, recorded from the Django app this replaced.
ALTER TABLE "cycles" ADD COLUMN "sort_order" double precision DEFAULT 65535.0 NOT NULL;
ALTER TABLE "cycles" ALTER COLUMN "sort_order" DROP DEFAULT;
ALTER TABLE "issue_comments" ADD COLUMN "access" varchar(100) DEFAULT 'INTERNAL' NOT NULL;
ALTER TABLE "issue_comments" ALTER COLUMN "access" DROP DEFAULT;
ALTER TABLE "modules" ADD COLUMN "sort_order" double precision DEFAULT 65535.0 NOT NULL;
ALTER TABLE "modules" ALTER COLUMN "sort_order" DROP DEFAULT;
ALTER TABLE "users" ADD COLUMN "display_name" varchar(255) DEFAULT '' NOT NULL;
ALTER TABLE "users" ALTER COLUMN "display_name" DROP DEFAULT;
CREATE TABLE "exporters" ("created_at" timestamp with time zone NOT NULL, "updated_at" timestamp with time zone NOT NULL, "id" uuid NOT NULL PRIMARY KEY, "project" uuid[] NULL, "provider" varchar(50) NOT NULL, "status" varchar(50) NOT NULL, "reason" text NOT NULL, "key" text NOT NULL, "url" varchar(800) NULL, "token" varchar(255) NOT NULL UNIQUE, "created_by_id" uuid NULL, "initiated_by_id" uuid NOT NULL, "updated_by_id" uuid NULL, "workspace_id" uuid NOT NULL);
CREATE TABLE "project_deploy_boards" ("created_at" timestamp with time zone NOT NULL, "updated_at" timestamp with time zone NOT NULL, "id" uuid NOT NULL PRIMARY KEY, "anchor" varchar(255) NOT NULL UNIQUE, "comments" boolean NOT NULL, "reactions" boolean NOT NULL, "votes" boolean NOT NULL, "views" jsonb NOT NULL, "created_by_id" uuid NULL, "inbox_id" uuid NULL, "project_id" uuid NOT NULL, "updated_by_id" uuid NULL, "workspace_id" uuid NOT NULL);
CREATE TABLE "issue_votes" ("created_at" timestamp with time zone NOT NULL, "updated_at" timestamp with time zone NOT NULL, "id" uuid NOT NULL PRIMARY KEY, "vote" integer NOT NULL, "actor_id" uuid NOT NULL, "created_by_id" uuid NULL, "issue_id" uuid NOT NULL, "project_id" uuid NOT NULL, "updated_by_id" uuid NULL, "workspace_id" uuid NOT NULL);
-- RUN db.0041_cycle_sort_order_issuecomment_access_and_more.generate_display_name
-- RUN db.0041_cycle_sort_order_issuecomment_access_and_more.rectify_field_issue_activity
-- RUN db.0041_cycle_sort_order_issuecomment_access_and_more.update_assignee_issue_activity
-- RUN db.0041_cycle_sort_order_issuecomment_access_and_more.update_name_activity
-- RUN db.0041_cycle_sort_order_issuecomment_access_and_more.random_cycle_order
-- RUN db.0041_cycle_sort_order_issuecomment_access_and_more.random_module_order
-- RUN db.0041_cycle_sort_order_issuecomment_access_and_more.update_user_issue_properties
-- RUN db.0041_cycle_sort_order_issuecomment_access_and_more.workspace_member_properties
ALTER TABLE "exporters" ADD CONSTRAINT "exporters_created_by_id_44e1d9b3_fk_users_id" FOREIGN KEY ("created_by_id") REFERENCES "users" ("id") DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE "exporters" ADD CONSTRAINT "exporters_initiated_by_id_d51f7552_fk_users_id" FOREIGN KEY ("initiated_by_id") REFERENCES "users" ("id") DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE "exporters" ADD CONSTRAINT "exporters_updated_by_id_d2572861_fk_users_id" FOREIGN KEY ("updated_by_id") REFERENCES "users" ("id") DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE "exporters" ADD CONSTRAINT "exporters_workspace_id_11a04317_fk_workspaces_id" FOREIGN KEY ("workspace_id") REFERENCES "workspaces" ("id") DEFERRABLE INITIALLY DEFERRED;
CREATE INDEX "exporters_token_c774aeeb_like" ON "exporters" ("token" varchar_pattern_ops);
CREATE INDEX "exporters_created_by_id_44e1d9b3" ON "exporters" ("created_by_id");
CREATE INDEX "exporters_initiated_by_id_d51f7552" ON "exporters" ("initiated_by_id");
CREATE INDEX "exporters_updated_by_id_d2572861" ON "exporters" ("updated_by_id");
CREATE INDEX "exporters_workspace_id_11a04317" ON "exporters" ("workspace_id");
ALTER TABLE "project_deploy_boards" ADD CONSTRAINT "project_deploy_boards_project_id_anchor_893d365a_uniq" UNIQUE ("project_id", "anchor");
ALTER TABLE "project_deploy_boards" ADD CONSTRAINT "project_deploy_boards_created_by_id_2ea72f98_fk_users_id" FOREIGN KEY ("created_by_id") REFERENCES "users" ("id") DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE "project_deploy_boards" ADD CONSTRAINT "project_deploy_boards_inbox_id_a6a75525_fk_inboxes_id" FOREIGN KEY ("inbox_id") REFERENCES "inboxes" ("id") DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE "project_deploy_boards" ADD CONSTRAINT "project_deploy_boards_project_id_49d887b2_fk_projects_id" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE "project_deploy_boards" ADD CONSTRAINT "project_deploy_boards_updated_by_id_290eb99e_fk_users_id" FOREIGN KEY ("updated_by_id") REFERENCES "users" ("id") DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE "project_deploy_boards" ADD CONSTRAINT "project_deploy_boards_workspace_id_cd92f164_fk_workspaces_id" FOREIGN KEY ("workspace_id") REFERENCES "workspaces" ("id") DEFERRABLE INITIALLY DEFERRED;
CREATE INDEX "project_deploy_boards_anchor_b61b8817_like" ON "project_deploy_boards" ("anchor" varchar_pattern_ops);
CREATE INDEX "project_deploy_boards_created_by_id_2ea72f98" ON "project_deploy_boards" ("created_by_id");
CREATE INDEX "project_deploy_boards_inbox_id_a6a75525" ON "project_deploy_boards" ("inbox_id");
CREATE INDEX "project_deploy_boards_project_id_49d887b2" ON "project_deploy_boards" ("project_id");
CREATE INDEX "project_deploy_boards_updated_by_id_290eb99e" ON "project_deploy_boards" ("updated_by_id");
CREATE INDEX "project_deploy_boards_workspace_id_cd92f164" ON "project_deploy_boards" ("workspace_id");
ALTER TABLE "issue_votes" ADD CONSTRAINT "issue_votes_issue_id_actor_id_086954ee_uniq" UNIQUE ("issue_id", "actor_id");
ALTER TABLE "issue_votes" ADD CONSTRAINT "issue_votes_actor_id_525cab61_fk_users_id" FOREIGN KEY ("actor_id") REFERENCES "users" ("id") DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE "issue_votes" ADD CONSTRAINT "issue_votes_created_by_id_86adcf5c_fk_users_id" FOREIGN KEY ("created_by_id") REFERENCES "users" ("id") DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE "issue_votes" ADD CONSTRAINT "issue_votes_issue_id_07a61ecb_fk_issues_id" FOREIGN KEY ("issue_id") REFERENCES "issues" ("id") DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE "issue_votes" ADD CONSTRAINT "issue_votes_project_id_b649f55b_fk_projects_id" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE "issue_votes" ADD CONSTRAINT "issue_votes_updated_by_id_9e2a6cdc_fk_users_id" FOREIGN KEY ("updated_by_id") REFERENCES "users" ("id") DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE "issue_votes" ADD CONSTRAINT "issue_votes_workspace_id_a3e91a6b_fk_workspaces_id" FOREIGN KEY ("workspace_id") REFERENCES "workspaces" ("id") DEFERRABLE INITIALLY DEFERRED;
CREATE INDEX "issue_votes_actor_id_525cab61" ON "issue_votes" ("actor_id");
CREATE INDEX "issue_votes_created_by_id_86adcf5c" ON "issue_votes" ("created_by_id");
CREATE INDEX "issue_votes_issue_id_07a61ecb" ON "issue_votes" ("issue_id");
CREATE INDEX "issue_votes_project_id_b649f55b" ON "issue_votes" ("project_id");
CREATE INDEX "issue_votes_updated_by_id_9e2a6cdc" ON "issue_votes" ("updated_by_id");
CREATE INDEX "issue_votes_workspace_id_a3e91a6b" ON "issue_votes" ("workspace_id");
