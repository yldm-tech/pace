-- django_celery_beat.0014_remove_clockedschedule_enabled, recorded by apps/api-go/tools/generate_migration_sql.py. Do not edit by hand.
ALTER TABLE "django_celery_beat_clockedschedule" DROP COLUMN "enabled" CASCADE;
