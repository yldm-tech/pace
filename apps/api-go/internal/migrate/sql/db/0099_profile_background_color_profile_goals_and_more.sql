-- db.0099_profile_background_color_profile_goals_and_more, recorded from the Django app this replaced.
ALTER TABLE "profiles" ADD COLUMN "background_color" varchar(255) DEFAULT '#4a9B8c' NOT NULL;
ALTER TABLE "profiles" ALTER COLUMN "background_color" DROP DEFAULT;
ALTER TABLE "profiles" ADD COLUMN "goals" jsonb DEFAULT '{}'::jsonb NOT NULL;
ALTER TABLE "profiles" ALTER COLUMN "goals" DROP DEFAULT;
ALTER TABLE "workspaces" ADD COLUMN "background_color" varchar(255) DEFAULT '#Bf9abB' NOT NULL;
ALTER TABLE "workspaces" ALTER COLUMN "background_color" DROP DEFAULT;
