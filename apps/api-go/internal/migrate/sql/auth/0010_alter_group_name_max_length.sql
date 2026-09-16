-- auth.0010_alter_group_name_max_length, recorded by apps/api-go/tools/generate_migration_sql.py. Do not edit by hand.
ALTER TABLE "auth_group" ALTER COLUMN "name" TYPE varchar(150);
