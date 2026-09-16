-- db.0019_auto_20230131_0049, recorded by apps/api-go/tools/generate_migration_sql.py. Do not edit by hand.
ALTER TABLE "issue_activities" ALTER COLUMN "new_value" TYPE text USING "new_value"::text;
ALTER TABLE "issue_activities" ALTER COLUMN "old_value" TYPE text USING "old_value"::text;
