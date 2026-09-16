-- django_celery_beat.0007_auto_20180521_0826, recorded by apps/api-go/tools/generate_migration_sql.py. Do not edit by hand.
ALTER TABLE "django_celery_beat_periodictask" ADD COLUMN "one_off" boolean DEFAULT false NOT NULL;
ALTER TABLE "django_celery_beat_periodictask" ALTER COLUMN "one_off" DROP DEFAULT;
ALTER TABLE "django_celery_beat_periodictask" ADD COLUMN "start_time" timestamp with time zone NULL;
