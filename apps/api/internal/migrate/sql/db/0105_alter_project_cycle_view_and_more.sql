-- db.0105_alter_project_cycle_view_and_more, recorded from the Django app this replaced.
CREATE INDEX "sessions_user_id_05e26f4a" ON "sessions" ("user_id");
CREATE INDEX "sessions_user_id_05e26f4a_like" ON "sessions" ("user_id" varchar_pattern_ops);
