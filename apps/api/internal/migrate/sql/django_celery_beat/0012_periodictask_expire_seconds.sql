-- django_celery_beat.0012_periodictask_expire_seconds, recorded from the Django app this replaced.
ALTER TABLE "django_celery_beat_periodictask" ADD COLUMN "expire_seconds" integer NULL CHECK ("expire_seconds" >= 0);
