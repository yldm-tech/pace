-- db.0097_project_external_id_project_external_source, recorded from the Django app this replaced.
ALTER TABLE "projects" ADD COLUMN "external_id" varchar(255) NULL;
ALTER TABLE "projects" ADD COLUMN "external_source" varchar(255) NULL;
