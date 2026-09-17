-- db.0032_auto_20230520_2015, recorded from the Django app this replaced.
ALTER TABLE "projects" RENAME COLUMN "icon" TO "emoji";
ALTER TABLE "projects" ADD COLUMN "icon_prop" jsonb NULL;
