-- db.0066_account_id_token_cycle_logo_props_module_logo_props, recorded from the Django app this replaced.
ALTER TABLE "accounts" ADD COLUMN "id_token" text DEFAULT '' NOT NULL;
ALTER TABLE "accounts" ALTER COLUMN "id_token" DROP DEFAULT;
ALTER TABLE "cycles" ADD COLUMN "logo_props" jsonb DEFAULT '{}'::jsonb NOT NULL;
ALTER TABLE "cycles" ALTER COLUMN "logo_props" DROP DEFAULT;
ALTER TABLE "modules" ADD COLUMN "logo_props" jsonb DEFAULT '{}'::jsonb NOT NULL;
ALTER TABLE "modules" ALTER COLUMN "logo_props" DROP DEFAULT;
ALTER TABLE "issue_views" ADD COLUMN "logo_props" jsonb DEFAULT '{}'::jsonb NOT NULL;
ALTER TABLE "issue_views" ALTER COLUMN "logo_props" DROP DEFAULT;
ALTER TABLE "inboxes" ADD COLUMN "logo_props" jsonb DEFAULT '{}'::jsonb NOT NULL;
ALTER TABLE "inboxes" ALTER COLUMN "logo_props" DROP DEFAULT;
ALTER TABLE "dashboards" ADD COLUMN "logo_props" jsonb DEFAULT '{}'::jsonb NOT NULL;
ALTER TABLE "dashboards" ALTER COLUMN "logo_props" DROP DEFAULT;
ALTER TABLE "widgets" ADD COLUMN "logo_props" jsonb DEFAULT '{}'::jsonb NOT NULL;
ALTER TABLE "widgets" ALTER COLUMN "logo_props" DROP DEFAULT;
ALTER TABLE "issues" ADD COLUMN "description_binary" bytea NULL;
ALTER TABLE "teams" ADD COLUMN "logo_props" jsonb DEFAULT '{}'::jsonb NOT NULL;
ALTER TABLE "teams" ALTER COLUMN "logo_props" DROP DEFAULT;
