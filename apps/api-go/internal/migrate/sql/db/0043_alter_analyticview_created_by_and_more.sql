-- db.0043_alter_analyticview_created_by_and_more, recorded from the Django app this replaced.
CREATE TABLE "issue_relations" ("created_at" timestamp with time zone NOT NULL, "updated_at" timestamp with time zone NOT NULL, "id" uuid NOT NULL PRIMARY KEY, "relation_type" varchar(20) NOT NULL, "created_by_id" uuid NULL, "issue_id" uuid NOT NULL, "project_id" uuid NOT NULL, "related_issue_id" uuid NOT NULL, "updated_by_id" uuid NULL, "workspace_id" uuid NOT NULL);
ALTER TABLE "issues" ADD COLUMN "is_draft" boolean DEFAULT false NOT NULL;
ALTER TABLE "issues" ALTER COLUMN "is_draft" DROP DEFAULT;
ALTER TABLE "issues" ALTER COLUMN "priority" SET DEFAULT 'none';
UPDATE "issues" SET "priority" = 'none' WHERE "priority" IS NULL; SET CONSTRAINTS ALL IMMEDIATE;
ALTER TABLE "issues" ALTER COLUMN "priority" SET NOT NULL;
ALTER TABLE "issues" ALTER COLUMN "priority" DROP DEFAULT;
-- RUN db.0043_alter_analyticview_created_by_and_more.create_issue_relation
-- RUN db.0043_alter_analyticview_created_by_and_more.update_issue_priority_choice
ALTER TABLE "issue_relations" ADD CONSTRAINT "issue_relations_issue_id_related_issue_id_640e8062_uniq" UNIQUE ("issue_id", "related_issue_id");
ALTER TABLE "issue_relations" ADD CONSTRAINT "issue_relations_created_by_id_854d07e7_fk_users_id" FOREIGN KEY ("created_by_id") REFERENCES "users" ("id") DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE "issue_relations" ADD CONSTRAINT "issue_relations_issue_id_e1db6f72_fk_issues_id" FOREIGN KEY ("issue_id") REFERENCES "issues" ("id") DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE "issue_relations" ADD CONSTRAINT "issue_relations_project_id_15350161_fk_projects_id" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE "issue_relations" ADD CONSTRAINT "issue_relations_related_issue_id_e1ea44a7_fk_issues_id" FOREIGN KEY ("related_issue_id") REFERENCES "issues" ("id") DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE "issue_relations" ADD CONSTRAINT "issue_relations_updated_by_id_3dfa850f_fk_users_id" FOREIGN KEY ("updated_by_id") REFERENCES "users" ("id") DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE "issue_relations" ADD CONSTRAINT "issue_relations_workspace_id_00b50e90_fk_workspaces_id" FOREIGN KEY ("workspace_id") REFERENCES "workspaces" ("id") DEFERRABLE INITIALLY DEFERRED;
CREATE INDEX "issue_relations_created_by_id_854d07e7" ON "issue_relations" ("created_by_id");
CREATE INDEX "issue_relations_issue_id_e1db6f72" ON "issue_relations" ("issue_id");
CREATE INDEX "issue_relations_project_id_15350161" ON "issue_relations" ("project_id");
CREATE INDEX "issue_relations_related_issue_id_e1ea44a7" ON "issue_relations" ("related_issue_id");
CREATE INDEX "issue_relations_updated_by_id_3dfa850f" ON "issue_relations" ("updated_by_id");
CREATE INDEX "issue_relations_workspace_id_00b50e90" ON "issue_relations" ("workspace_id");
