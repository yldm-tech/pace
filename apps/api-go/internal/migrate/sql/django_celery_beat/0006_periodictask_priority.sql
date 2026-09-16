-- django_celery_beat.0006_periodictask_priority, recorded by apps/api-go/tools/generate_migration_sql.py. Do not edit by hand.
ALTER TABLE "django_celery_beat_periodictask" ADD COLUMN "priority" integer NULL CHECK ("priority" >= 0);
