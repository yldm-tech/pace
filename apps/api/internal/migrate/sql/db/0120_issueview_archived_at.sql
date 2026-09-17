-- db.0120_issueview_archived_at, recorded from the Django app this replaced.
ALTER TABLE "issue_views" ADD COLUMN "archived_at" timestamp with time zone NULL;
