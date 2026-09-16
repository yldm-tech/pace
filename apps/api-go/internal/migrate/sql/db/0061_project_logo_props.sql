-- db.0061_project_logo_props, recorded by apps/api-go/tools/generate_migration_sql.py. Do not edit by hand.
ALTER TABLE "issue_links" ALTER COLUMN "url" TYPE text USING "url"::text;
ALTER TABLE "projects" ADD COLUMN "logo_props" jsonb DEFAULT '{}'::jsonb NOT NULL;
ALTER TABLE "projects" ALTER COLUMN "logo_props" DROP DEFAULT;
