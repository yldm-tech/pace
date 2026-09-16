-- db.0042_alter_analyticview_created_by_and_more, recorded from the Django app this replaced.
-- RUN db.0042_alter_analyticview_created_by_and_more.update_user_timezones
CREATE TABLE "project_public_members" ("created_at" timestamp with time zone NOT NULL, "updated_at" timestamp with time zone NOT NULL, "id" uuid NOT NULL PRIMARY KEY, "created_by_id" uuid NULL, "member_id" uuid NOT NULL, "project_id" uuid NOT NULL, "updated_by_id" uuid NULL, "workspace_id" uuid NOT NULL);
ALTER TABLE "project_public_members" ADD CONSTRAINT "project_public_members_project_id_member_id_51cd09a4_uniq" UNIQUE ("project_id", "member_id");
ALTER TABLE "project_public_members" ADD CONSTRAINT "project_public_members_created_by_id_c4c7c776_fk_users_id" FOREIGN KEY ("created_by_id") REFERENCES "users" ("id") DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE "project_public_members" ADD CONSTRAINT "project_public_members_member_id_52f257f9_fk_users_id" FOREIGN KEY ("member_id") REFERENCES "users" ("id") DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE "project_public_members" ADD CONSTRAINT "project_public_members_project_id_2dfd893d_fk_projects_id" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE "project_public_members" ADD CONSTRAINT "project_public_members_updated_by_id_c3e4d675_fk_users_id" FOREIGN KEY ("updated_by_id") REFERENCES "users" ("id") DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE "project_public_members" ADD CONSTRAINT "project_public_members_workspace_id_ebfce110_fk_workspaces_id" FOREIGN KEY ("workspace_id") REFERENCES "workspaces" ("id") DEFERRABLE INITIALLY DEFERRED;
CREATE INDEX "project_public_members_created_by_id_c4c7c776" ON "project_public_members" ("created_by_id");
CREATE INDEX "project_public_members_member_id_52f257f9" ON "project_public_members" ("member_id");
CREATE INDEX "project_public_members_project_id_2dfd893d" ON "project_public_members" ("project_id");
CREATE INDEX "project_public_members_updated_by_id_c3e4d675" ON "project_public_members" ("updated_by_id");
CREATE INDEX "project_public_members_workspace_id_ebfce110" ON "project_public_members" ("workspace_id");
