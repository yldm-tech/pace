-- db.0060_cycle_progress_snapshot, recorded from the Django app this replaced.
ALTER TABLE "cycles" ADD COLUMN "progress_snapshot" jsonb DEFAULT '{}'::jsonb NOT NULL;
ALTER TABLE "cycles" ALTER COLUMN "progress_snapshot" DROP DEFAULT;
