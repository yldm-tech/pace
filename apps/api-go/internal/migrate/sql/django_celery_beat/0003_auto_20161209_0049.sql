-- django_celery_beat.0003_auto_20161209_0049, recorded by apps/api-go/tools/generate_migration_sql.py. Do not edit by hand.
ALTER TABLE "django_celery_beat_solarschedule" ADD CONSTRAINT "django_celery_beat_solar_event_latitude_longitude_ba64999a_uniq" UNIQUE ("event", "latitude", "longitude");
