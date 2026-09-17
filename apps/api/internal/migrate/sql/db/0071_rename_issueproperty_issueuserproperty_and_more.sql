-- db.0071_rename_issueproperty_issueuserproperty_and_more, recorded from the Django app this replaced.
ALTER TABLE "issue_properties" RENAME TO "issue_user_properties";
ALTER TABLE "issue_types" ADD COLUMN "is_active" boolean DEFAULT true NOT NULL;
ALTER TABLE "issue_types" ALTER COLUMN "is_active" DROP DEFAULT;
ALTER TABLE "projects" ADD COLUMN "is_issue_type_enabled" boolean DEFAULT false NOT NULL;
ALTER TABLE "projects" ALTER COLUMN "is_issue_type_enabled" DROP DEFAULT;
