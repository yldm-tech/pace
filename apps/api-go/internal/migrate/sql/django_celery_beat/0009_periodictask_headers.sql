-- django_celery_beat.0009_periodictask_headers, recorded from the Django app this replaced.
ALTER TABLE "django_celery_beat_periodictask" ADD COLUMN "headers" text DEFAULT '{}' NOT NULL;
ALTER TABLE "django_celery_beat_periodictask" ALTER COLUMN "headers" DROP DEFAULT;
