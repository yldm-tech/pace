-- db.0105_alter_project_cycle_view_and_more, recorded by apps/api-go/tools/generate_migration_sql.py. Do not edit by hand.
CREATE INDEX "sessions_user_id_05e26f4a" ON "sessions" ("user_id");
CREATE INDEX "sessions_user_id_05e26f4a_like" ON "sessions" ("user_id" varchar_pattern_ops);
