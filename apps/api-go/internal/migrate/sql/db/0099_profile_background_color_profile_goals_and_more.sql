-- db.0099_profile_background_color_profile_goals_and_more, recorded by apps/api-go/tools/generate_migration_sql.py. Do not edit by hand.
ALTER TABLE "profiles" ADD COLUMN "background_color" varchar(255) DEFAULT '#4a9B8c' NOT NULL;
ALTER TABLE "profiles" ALTER COLUMN "background_color" DROP DEFAULT;
ALTER TABLE "profiles" ADD COLUMN "goals" jsonb DEFAULT '{}'::jsonb NOT NULL;
ALTER TABLE "profiles" ALTER COLUMN "goals" DROP DEFAULT;
ALTER TABLE "workspaces" ADD COLUMN "background_color" varchar(255) DEFAULT '#Bf9abB' NOT NULL;
ALTER TABLE "workspaces" ALTER COLUMN "background_color" DROP DEFAULT;
