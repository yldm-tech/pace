-- db.0095_page_external_id_page_external_source, recorded by apps/api-go/tools/generate_migration_sql.py. Do not edit by hand.
ALTER TABLE "pages" ADD COLUMN "external_id" varchar(255) NULL;
ALTER TABLE "pages" ADD COLUMN "external_source" varchar(255) NULL;
