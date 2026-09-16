-- db.0109_issuecomment_description_and_parent_id, recorded from the Django app this replaced.
ALTER TABLE "issue_comments" ADD COLUMN "description_id" uuid NULL UNIQUE CONSTRAINT "issue_comments_description_id_0cb72512_fk_descriptions_id" REFERENCES "descriptions"("id") DEFERRABLE INITIALLY DEFERRED; SET CONSTRAINTS "issue_comments_description_id_0cb72512_fk_descriptions_id" IMMEDIATE;
ALTER TABLE "issue_comments" ADD COLUMN "parent_id" uuid NULL CONSTRAINT "issue_comments_parent_id_d8db10b1_fk_issue_comments_id" REFERENCES "issue_comments"("id") DEFERRABLE INITIALLY DEFERRED; SET CONSTRAINTS "issue_comments_parent_id_d8db10b1_fk_issue_comments_id" IMMEDIATE;
CREATE INDEX "issue_comments_parent_id_d8db10b1" ON "issue_comments" ("parent_id");
