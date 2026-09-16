-- django_celery_beat.0014_remove_clockedschedule_enabled, recorded from the Django app this replaced.
ALTER TABLE "django_celery_beat_clockedschedule" DROP COLUMN "enabled" CASCADE;
