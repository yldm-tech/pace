-- license.0004_changelog_deleted_at_instance_deleted_at_and_more, recorded by apps/api-go/tools/generate_migration_sql.py. Do not edit by hand.
ALTER TABLE "changelogs" ADD COLUMN "deleted_at" timestamp with time zone NULL;
ALTER TABLE "instances" ADD COLUMN "deleted_at" timestamp with time zone NULL;
ALTER TABLE "instance_admins" ADD COLUMN "deleted_at" timestamp with time zone NULL;
ALTER TABLE "instance_configurations" ADD COLUMN "deleted_at" timestamp with time zone NULL;
