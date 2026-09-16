-- db.0094_auto_20250425_0902, recorded from the Django app this replaced.
-- RUN db.0094_auto_20250425_0902.set_default_source_type
ALTER TABLE "profiles" ADD COLUMN "start_of_the_week" smallint DEFAULT 0 NOT NULL CHECK ("start_of_the_week" >= 0);
ALTER TABLE "profiles" ALTER COLUMN "start_of_the_week" DROP DEFAULT;
