-- db.0028_auto_20230414_1703, recorded by apps/api-go/tools/generate_migration_sql.py. Do not edit by hand.
ALTER TABLE "users" ADD COLUMN "theme" jsonb DEFAULT '{}'::jsonb NOT NULL;
ALTER TABLE "users" ALTER COLUMN "theme" DROP DEFAULT;
ALTER TABLE "issues" ALTER COLUMN "estimate_point" DROP NOT NULL;
CREATE TABLE "workspace_themes" ("created_at" timestamp with time zone NOT NULL, "updated_at" timestamp with time zone NOT NULL, "id" uuid NOT NULL PRIMARY KEY, "name" varchar(300) NOT NULL, "colors" jsonb NOT NULL, "actor_id" uuid NOT NULL, "created_by_id" uuid NULL, "updated_by_id" uuid NULL, "workspace_id" uuid NOT NULL);
ALTER TABLE "workspace_themes" ADD CONSTRAINT "workspace_themes_workspace_id_name_500d4836_uniq" UNIQUE ("workspace_id", "name");
ALTER TABLE "workspace_themes" ADD CONSTRAINT "workspace_themes_actor_id_0e94172e_fk_users_id" FOREIGN KEY ("actor_id") REFERENCES "users" ("id") DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE "workspace_themes" ADD CONSTRAINT "workspace_themes_created_by_id_676e2655_fk_users_id" FOREIGN KEY ("created_by_id") REFERENCES "users" ("id") DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE "workspace_themes" ADD CONSTRAINT "workspace_themes_updated_by_id_bba863fe_fk_users_id" FOREIGN KEY ("updated_by_id") REFERENCES "users" ("id") DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE "workspace_themes" ADD CONSTRAINT "workspace_themes_workspace_id_d1bffad8_fk_workspaces_id" FOREIGN KEY ("workspace_id") REFERENCES "workspaces" ("id") DEFERRABLE INITIALLY DEFERRED;
CREATE INDEX "workspace_themes_actor_id_0e94172e" ON "workspace_themes" ("actor_id");
CREATE INDEX "workspace_themes_created_by_id_676e2655" ON "workspace_themes" ("created_by_id");
CREATE INDEX "workspace_themes_updated_by_id_bba863fe" ON "workspace_themes" ("updated_by_id");
CREATE INDEX "workspace_themes_workspace_id_d1bffad8" ON "workspace_themes" ("workspace_id");
