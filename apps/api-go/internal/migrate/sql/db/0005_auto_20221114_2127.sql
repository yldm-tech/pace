-- db.0005_auto_20221114_2127, recorded by apps/api-go/tools/generate_migration_sql.py. Do not edit by hand.
ALTER TABLE "cycle" ALTER COLUMN "end_date" DROP NOT NULL;
ALTER TABLE "cycle" ALTER COLUMN "start_date" DROP NOT NULL;
