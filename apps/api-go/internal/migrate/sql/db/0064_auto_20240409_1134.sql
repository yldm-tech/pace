-- db.0064_auto_20240409_1134, recorded by apps/api-go/tools/generate_migration_sql.py. Do not edit by hand.
ALTER TABLE "pages" ADD COLUMN "view_props" jsonb DEFAULT '{"full_width": false}'::jsonb NOT NULL;
ALTER TABLE "pages" ALTER COLUMN "view_props" DROP DEFAULT;
