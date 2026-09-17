-- db.0005_auto_20221114_2127, recorded from the Django app this replaced.
ALTER TABLE "cycle" ALTER COLUMN "end_date" DROP NOT NULL;
ALTER TABLE "cycle" ALTER COLUMN "start_date" DROP NOT NULL;
