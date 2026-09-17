-- db.0064_auto_20240409_1134, recorded from the Django app this replaced.
ALTER TABLE "pages" ADD COLUMN "view_props" jsonb DEFAULT '{"full_width": false}'::jsonb NOT NULL;
ALTER TABLE "pages" ALTER COLUMN "view_props" DROP DEFAULT;
