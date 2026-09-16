-- db.0089_workspacehomepreference_and_more, recorded by apps/api-go/tools/generate_migration_sql.py. Do not edit by hand.
CREATE TABLE "workspace_home_preferences" ("created_at" timestamp with time zone NOT NULL, "updated_at" timestamp with time zone NOT NULL, "deleted_at" timestamp with time zone NULL, "id" uuid NOT NULL PRIMARY KEY, "key" varchar(255) NOT NULL, "is_enabled" boolean NOT NULL, "config" jsonb NOT NULL, "created_by_id" uuid NULL, "updated_by_id" uuid NULL, "user_id" uuid NOT NULL, "workspace_id" uuid NOT NULL);
CREATE UNIQUE INDEX "workspace_user_home_preferences_unique_workspace_user_key_when_deleted_at_null" ON "workspace_home_preferences" ("workspace_id", "user_id", "key") WHERE "deleted_at" IS NULL;
ALTER TABLE "workspace_home_preferences" ADD CONSTRAINT "workspace_home_preferenc_workspace_id_user_id_key_75ea36d3_uniq" UNIQUE ("workspace_id", "user_id", "key", "deleted_at");
ALTER TABLE "pages" ALTER COLUMN "name" TYPE text USING "name"::text;
ALTER TABLE "stickies" ALTER COLUMN "name" DROP NOT NULL;
ALTER TABLE "workspace_home_preferences" ADD COLUMN "sort_order" integer DEFAULT 65535 NOT NULL CHECK ("sort_order" >= 0);
ALTER TABLE "workspace_home_preferences" ALTER COLUMN "sort_order" DROP DEFAULT;
ALTER TABLE "workspace_home_preferences" ADD CONSTRAINT "workspace_home_preferences_created_by_id_f31fc163_fk_users_id" FOREIGN KEY ("created_by_id") REFERENCES "users" ("id") DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE "workspace_home_preferences" ADD CONSTRAINT "workspace_home_preferences_updated_by_id_14ed118a_fk_users_id" FOREIGN KEY ("updated_by_id") REFERENCES "users" ("id") DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE "workspace_home_preferences" ADD CONSTRAINT "workspace_home_preferences_user_id_4087938d_fk_users_id" FOREIGN KEY ("user_id") REFERENCES "users" ("id") DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE "workspace_home_preferences" ADD CONSTRAINT "workspace_home_prefe_workspace_id_b49f76e0_fk_workspace" FOREIGN KEY ("workspace_id") REFERENCES "workspaces" ("id") DEFERRABLE INITIALLY DEFERRED;
CREATE INDEX "workspace_home_preferences_created_by_id_f31fc163" ON "workspace_home_preferences" ("created_by_id");
CREATE INDEX "workspace_home_preferences_updated_by_id_14ed118a" ON "workspace_home_preferences" ("updated_by_id");
CREATE INDEX "workspace_home_preferences_user_id_4087938d" ON "workspace_home_preferences" ("user_id");
CREATE INDEX "workspace_home_preferences_workspace_id_b49f76e0" ON "workspace_home_preferences" ("workspace_id");
