-- django_celery_beat.0012_periodictask_expire_seconds, recorded by apps/api-go/tools/generate_migration_sql.py. Do not edit by hand.
ALTER TABLE "django_celery_beat_periodictask" ADD COLUMN "expire_seconds" integer NULL CHECK ("expire_seconds" >= 0);
