-- db.0069_alter_account_provider_and_more, recorded from the Django app this replaced.
ALTER TABLE "issue_views" ADD COLUMN "is_locked" boolean DEFAULT false NOT NULL;
ALTER TABLE "issue_views" ALTER COLUMN "is_locked" DROP DEFAULT;
ALTER TABLE "issue_views" ADD COLUMN "owned_by_id" uuid NULL CONSTRAINT "issue_views_owned_by_id_5e261e5d_fk_users_id" REFERENCES "users"("id") DEFERRABLE INITIALLY DEFERRED; SET CONSTRAINTS "issue_views_owned_by_id_5e261e5d_fk_users_id" IMMEDIATE;
-- RUN db.0069_alter_account_provider_and_more.populate_views_owned_by
SET CONSTRAINTS "issue_views_owned_by_id_5e261e5d_fk_users_id" IMMEDIATE; ALTER TABLE "issue_views" DROP CONSTRAINT "issue_views_owned_by_id_5e261e5d_fk_users_id";
ALTER TABLE "issue_views" ALTER COLUMN "owned_by_id" SET NOT NULL;
ALTER TABLE "issue_views" ADD CONSTRAINT "issue_views_owned_by_id_5e261e5d_fk_users_id" FOREIGN KEY ("owned_by_id") REFERENCES "users" ("id") DEFERRABLE INITIALLY DEFERRED;
CREATE INDEX "issue_views_owned_by_id_5e261e5d" ON "issue_views" ("owned_by_id");
