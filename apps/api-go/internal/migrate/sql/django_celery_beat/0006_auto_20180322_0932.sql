-- django_celery_beat.0006_auto_20180322_0932, recorded from the Django app this replaced.
ALTER TABLE "django_celery_beat_crontabschedule" ADD COLUMN "timezone" varchar(63) DEFAULT 'UTC' NOT NULL;
ALTER TABLE "django_celery_beat_crontabschedule" ALTER COLUMN "timezone" DROP DEFAULT;
ALTER TABLE "django_celery_beat_crontabschedule" ALTER COLUMN "day_of_month" TYPE varchar(124);
ALTER TABLE "django_celery_beat_crontabschedule" ALTER COLUMN "hour" TYPE varchar(96);
ALTER TABLE "django_celery_beat_crontabschedule" ALTER COLUMN "minute" TYPE varchar(240);
