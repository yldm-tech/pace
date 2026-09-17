-- django_celery_beat.0006_periodictask_priority, recorded from the Django app this replaced.
ALTER TABLE "django_celery_beat_periodictask" ADD COLUMN "priority" integer NULL CHECK ("priority" >= 0);
