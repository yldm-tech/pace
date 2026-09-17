-- db.0091_issuecomment_edited_at_and_more, recorded from the Django app this replaced.
ALTER TABLE "issue_comments" ADD COLUMN "edited_at" timestamp with time zone NULL;
ALTER TABLE "profiles" ADD COLUMN "is_smooth_cursor_enabled" boolean DEFAULT false NOT NULL;
ALTER TABLE "profiles" ALTER COLUMN "is_smooth_cursor_enabled" DROP DEFAULT;
SET CONSTRAINTS "webhook_logs_webhook_id_53707b3d_fk_webhooks_id" IMMEDIATE; ALTER TABLE "webhook_logs" DROP CONSTRAINT "webhook_logs_webhook_id_53707b3d_fk_webhooks_id";
DROP INDEX IF EXISTS "webhook_logs_webhook_id_53707b3d";
ALTER TABLE "webhook_logs" RENAME COLUMN "webhook_id" TO "webhook";
