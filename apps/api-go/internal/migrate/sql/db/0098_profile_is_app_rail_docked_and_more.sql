-- db.0098_profile_is_app_rail_docked_and_more, recorded by apps/api-go/tools/generate_migration_sql.py. Do not edit by hand.
ALTER TABLE "profiles" ADD COLUMN "is_app_rail_docked" boolean DEFAULT true NOT NULL;
ALTER TABLE "profiles" ALTER COLUMN "is_app_rail_docked" DROP DEFAULT;
ALTER TABLE "comment_reactions" ALTER COLUMN "reaction" TYPE text USING "reaction"::text;
ALTER TABLE "issue_reactions" ALTER COLUMN "reaction" TYPE text USING "reaction"::text;
