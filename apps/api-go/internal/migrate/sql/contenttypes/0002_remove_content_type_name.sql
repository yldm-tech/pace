-- contenttypes.0002_remove_content_type_name, recorded from the Django app this replaced.
ALTER TABLE "django_content_type" ALTER COLUMN "name" DROP NOT NULL;
-- RUN contenttypes.0002_remove_content_type_name.noop
ALTER TABLE "django_content_type" DROP COLUMN "name" CASCADE;
