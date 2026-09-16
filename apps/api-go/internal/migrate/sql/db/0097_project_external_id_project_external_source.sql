-- db.0097_project_external_id_project_external_source, recorded by apps/api-go/tools/generate_migration_sql.py. Do not edit by hand.
ALTER TABLE "projects" ADD COLUMN "external_id" varchar(255) NULL;
ALTER TABLE "projects" ADD COLUMN "external_source" varchar(255) NULL;
