-- db.0034_auto_20230628_1046, recorded from the Django app this replaced.
SET CONSTRAINTS "issue_timeline_created_by_id_0cff104c_fk_user_id" IMMEDIATE; ALTER TABLE "issue_timelines" DROP CONSTRAINT "issue_timeline_created_by_id_0cff104c_fk_user_id";
ALTER TABLE "issue_timelines" DROP COLUMN "created_by_id" CASCADE;
SET CONSTRAINTS "issue_timeline_issue_id_0e4dc65a_fk_issue_id" IMMEDIATE; ALTER TABLE "issue_timelines" DROP CONSTRAINT "issue_timeline_issue_id_0e4dc65a_fk_issue_id";
ALTER TABLE "issue_timelines" DROP COLUMN "issue_id" CASCADE;
SET CONSTRAINTS "issue_timeline_project_id_320d52be_fk_project_id" IMMEDIATE; ALTER TABLE "issue_timelines" DROP CONSTRAINT "issue_timeline_project_id_320d52be_fk_project_id";
ALTER TABLE "issue_timelines" DROP COLUMN "project_id" CASCADE;
SET CONSTRAINTS "issue_timeline_updated_by_id_6c3e76b4_fk_user_id" IMMEDIATE; ALTER TABLE "issue_timelines" DROP CONSTRAINT "issue_timeline_updated_by_id_6c3e76b4_fk_user_id";
ALTER TABLE "issue_timelines" DROP COLUMN "updated_by_id" CASCADE;
SET CONSTRAINTS "issue_timeline_workspace_id_a23baf87_fk_workspace_id" IMMEDIATE; ALTER TABLE "issue_timelines" DROP CONSTRAINT "issue_timeline_workspace_id_a23baf87_fk_workspace_id";
ALTER TABLE "issue_timelines" DROP COLUMN "workspace_id" CASCADE;
DROP TABLE "shortcuts" CASCADE;
DROP TABLE "issue_timelines" CASCADE;
