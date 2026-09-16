-- db.0019_auto_20230131_0049, recorded from the Django app this replaced.
ALTER TABLE "issue_activities" ALTER COLUMN "new_value" TYPE text USING "new_value"::text;
ALTER TABLE "issue_activities" ALTER COLUMN "old_value" TYPE text USING "old_value"::text;
