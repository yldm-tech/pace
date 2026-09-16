-- db.0120_issueview_archived_at, recorded by apps/api-go/tools/generate_migration_sql.py. Do not edit by hand.
ALTER TABLE "issue_views" ADD COLUMN "archived_at" timestamp with time zone NULL;
