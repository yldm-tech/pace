-- db.0015_auto_20230107_1636, recorded from the Django app this replaced.
ALTER TABLE "issue_comments" RENAME COLUMN "comment" TO "comment_stripped";
ALTER TABLE "issue_comments" ADD COLUMN "comment_html" text DEFAULT '' NOT NULL;
ALTER TABLE "issue_comments" ALTER COLUMN "comment_html" DROP DEFAULT;
ALTER TABLE "issue_comments" ADD COLUMN "comment_json" jsonb NULL;
