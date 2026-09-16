-- django_celery_beat.0009_periodictask_headers, recorded by apps/api-go/tools/generate_migration_sql.py. Do not edit by hand.
ALTER TABLE "django_celery_beat_periodictask" ADD COLUMN "headers" text DEFAULT '{}' NOT NULL;
ALTER TABLE "django_celery_beat_periodictask" ALTER COLUMN "headers" DROP DEFAULT;
