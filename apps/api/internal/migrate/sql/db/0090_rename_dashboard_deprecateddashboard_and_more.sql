-- db.0090_rename_dashboard_deprecateddashboard_and_more, recorded from the Django app this replaced.
ALTER TABLE "workspace_home_preferences" DROP CONSTRAINT "workspace_home_preferences_sort_order_check";
ALTER TABLE "workspace_home_preferences" ALTER COLUMN "sort_order" TYPE double precision USING "sort_order"::double precision;
ALTER TABLE "dashboards" RENAME TO "deprecated_dashboards";
ALTER TABLE "dashboard_widgets" RENAME TO "deprecated_dashboard_widgets";
ALTER TABLE "widgets" RENAME TO "deprecated_widgets";
CREATE TABLE "workspace_user_preferences" ("created_at" timestamp with time zone NOT NULL, "updated_at" timestamp with time zone NOT NULL, "deleted_at" timestamp with time zone NULL, "id" uuid NOT NULL PRIMARY KEY, "key" varchar(255) NOT NULL, "is_pinned" boolean NOT NULL, "sort_order" double precision NOT NULL, "created_by_id" uuid NULL, "updated_by_id" uuid NULL, "user_id" uuid NOT NULL, "workspace_id" uuid NOT NULL);
CREATE UNIQUE INDEX "workspace_user_preferences_unique_workspace_user_key_when_deleted_at_null" ON "workspace_user_preferences" ("workspace_id", "user_id", "key") WHERE "deleted_at" IS NULL;
ALTER TABLE "workspace_user_preferences" ADD CONSTRAINT "workspace_user_preferenc_workspace_id_user_id_key_79341493_uniq" UNIQUE ("workspace_id", "user_id", "key", "deleted_at");
ALTER TABLE "workspace_user_preferences" ADD CONSTRAINT "workspace_user_preferences_created_by_id_2d566570_fk_users_id" FOREIGN KEY ("created_by_id") REFERENCES "users" ("id") DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE "workspace_user_preferences" ADD CONSTRAINT "workspace_user_preferences_updated_by_id_65fed266_fk_users_id" FOREIGN KEY ("updated_by_id") REFERENCES "users" ("id") DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE "workspace_user_preferences" ADD CONSTRAINT "workspace_user_preferences_user_id_0ba5007a_fk_users_id" FOREIGN KEY ("user_id") REFERENCES "users" ("id") DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE "workspace_user_preferences" ADD CONSTRAINT "workspace_user_prefe_workspace_id_a345adde_fk_workspace" FOREIGN KEY ("workspace_id") REFERENCES "workspaces" ("id") DEFERRABLE INITIALLY DEFERRED;
CREATE INDEX "workspace_user_preferences_created_by_id_2d566570" ON "workspace_user_preferences" ("created_by_id");
CREATE INDEX "workspace_user_preferences_updated_by_id_65fed266" ON "workspace_user_preferences" ("updated_by_id");
CREATE INDEX "workspace_user_preferences_user_id_0ba5007a" ON "workspace_user_preferences" ("user_id");
CREATE INDEX "workspace_user_preferences_workspace_id_a345adde" ON "workspace_user_preferences" ("workspace_id");
