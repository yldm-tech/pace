-- license.0006_instance_is_current_version_deprecated, recorded from the Django app this replaced.
ALTER TABLE "instances" ADD COLUMN "is_current_version_deprecated" boolean DEFAULT false NOT NULL;
ALTER TABLE "instances" ALTER COLUMN "is_current_version_deprecated" DROP DEFAULT;
