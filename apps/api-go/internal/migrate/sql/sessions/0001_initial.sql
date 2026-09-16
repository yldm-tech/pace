-- sessions.0001_initial, recorded by apps/api-go/tools/generate_migration_sql.py. Do not edit by hand.
CREATE TABLE "django_session" ("session_key" varchar(40) NOT NULL PRIMARY KEY, "session_data" text NOT NULL, "expire_date" timestamp with time zone NOT NULL);
CREATE INDEX "django_session_session_key_c0390e0f_like" ON "django_session" ("session_key" varchar_pattern_ops);
CREATE INDEX "django_session_expire_date_a5c62663" ON "django_session" ("expire_date");
