-- django_celery_beat.0007_auto_20180521_0826, recorded from the Django app this replaced.
ALTER TABLE "django_celery_beat_periodictask" ADD COLUMN "one_off" boolean DEFAULT false NOT NULL;
ALTER TABLE "django_celery_beat_periodictask" ALTER COLUMN "one_off" DROP DEFAULT;
ALTER TABLE "django_celery_beat_periodictask" ADD COLUMN "start_time" timestamp with time zone NULL;
