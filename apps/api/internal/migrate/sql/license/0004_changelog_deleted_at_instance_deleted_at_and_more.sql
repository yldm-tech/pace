-- license.0004_changelog_deleted_at_instance_deleted_at_and_more, recorded from the Django app this replaced.
ALTER TABLE "changelogs" ADD COLUMN "deleted_at" timestamp with time zone NULL;
ALTER TABLE "instances" ADD COLUMN "deleted_at" timestamp with time zone NULL;
ALTER TABLE "instance_admins" ADD COLUMN "deleted_at" timestamp with time zone NULL;
ALTER TABLE "instance_configurations" ADD COLUMN "deleted_at" timestamp with time zone NULL;
