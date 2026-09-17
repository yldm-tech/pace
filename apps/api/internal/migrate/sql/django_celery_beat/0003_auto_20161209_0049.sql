-- django_celery_beat.0003_auto_20161209_0049, recorded from the Django app this replaced.
ALTER TABLE "django_celery_beat_solarschedule" ADD CONSTRAINT "django_celery_beat_solar_event_latitude_longitude_ba64999a_uniq" UNIQUE ("event", "latitude", "longitude");
