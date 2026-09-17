-- db.0095_page_external_id_page_external_source, recorded from the Django app this replaced.
ALTER TABLE "pages" ADD COLUMN "external_id" varchar(255) NULL;
ALTER TABLE "pages" ADD COLUMN "external_source" varchar(255) NULL;
