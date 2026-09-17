-- db.0002_auto_20221104_2239, recorded from the Django app this replaced.
ALTER TABLE "project" RENAME COLUMN "description_rt" TO "description_text";
ALTER TABLE "issue_activity" ADD COLUMN "actor_id" uuid NULL CONSTRAINT "issue_activity_actor_id_52fdd42d_fk_user_id" REFERENCES "user"("id") DEFERRABLE INITIALLY DEFERRED; SET CONSTRAINTS "issue_activity_actor_id_52fdd42d_fk_user_id" IMMEDIATE;
ALTER TABLE "issue_comment" ADD COLUMN "actor_id" uuid NULL CONSTRAINT "issue_comment_actor_id_d312315b_fk_user_id" REFERENCES "user"("id") DEFERRABLE INITIALLY DEFERRED; SET CONSTRAINTS "issue_comment_actor_id_d312315b_fk_user_id" IMMEDIATE;
ALTER TABLE "state" ADD COLUMN "sequence" integer DEFAULT 65535 NOT NULL CHECK ("sequence" >= 0);
ALTER TABLE "state" ALTER COLUMN "sequence" DROP DEFAULT;
ALTER TABLE "workspace" ADD COLUMN "company_size" integer DEFAULT 10 NOT NULL CHECK ("company_size" >= 0);
ALTER TABLE "workspace" ALTER COLUMN "company_size" DROP DEFAULT;
ALTER TABLE "workspace_member" ADD COLUMN "company_role" text NULL;
SET CONSTRAINTS "cycle_issue_issue_id_fd06e284_fk_issue_id" IMMEDIATE; ALTER TABLE "cycle_issue" DROP CONSTRAINT "cycle_issue_issue_id_fd06e284_fk_issue_id";
DROP INDEX IF EXISTS "cycle_issue_issue_id_fd06e284";
ALTER TABLE "cycle_issue" ADD CONSTRAINT "cycle_issue_issue_id_fd06e284_uniq" UNIQUE ("issue_id");
ALTER TABLE "cycle_issue" ADD CONSTRAINT "cycle_issue_issue_id_fd06e284_fk_issue_id" FOREIGN KEY ("issue_id") REFERENCES "issue" ("id") DEFERRABLE INITIALLY DEFERRED;
CREATE INDEX "issue_activity_actor_id_52fdd42d" ON "issue_activity" ("actor_id");
CREATE INDEX "issue_comment_actor_id_d312315b" ON "issue_comment" ("actor_id");
