-- auth.0002_alter_permission_name_max_length, recorded by apps/api-go/tools/generate_migration_sql.py. Do not edit by hand.
ALTER TABLE "auth_permission" ALTER COLUMN "name" TYPE varchar(255);
