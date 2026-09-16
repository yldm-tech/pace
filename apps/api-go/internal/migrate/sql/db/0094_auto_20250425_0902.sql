-- db.0094_auto_20250425_0902, recorded by apps/api-go/tools/generate_migration_sql.py. Do not edit by hand.
ALTER TABLE "profiles" ADD COLUMN "start_of_the_week" smallint DEFAULT 0 NOT NULL CHECK ("start_of_the_week" >= 0);
ALTER TABLE "profiles" ALTER COLUMN "start_of_the_week" DROP DEFAULT;
