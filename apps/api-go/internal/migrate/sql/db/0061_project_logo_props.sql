-- db.0061_project_logo_props, recorded from the Django app this replaced.
ALTER TABLE "issue_links" ALTER COLUMN "url" TYPE text USING "url"::text;
ALTER TABLE "projects" ADD COLUMN "logo_props" jsonb DEFAULT '{}'::jsonb NOT NULL;
ALTER TABLE "projects" ALTER COLUMN "logo_props" DROP DEFAULT;
-- RUN db.0061_project_logo_props.update_project_logo_props
