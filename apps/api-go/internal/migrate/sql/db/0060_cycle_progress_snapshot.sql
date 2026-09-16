-- db.0060_cycle_progress_snapshot, recorded by apps/api-go/tools/generate_migration_sql.py. Do not edit by hand.
ALTER TABLE "cycles" ADD COLUMN "progress_snapshot" jsonb DEFAULT '{}'::jsonb NOT NULL;
ALTER TABLE "cycles" ALTER COLUMN "progress_snapshot" DROP DEFAULT;
