-- db.0093_page_moved_to_page_page_moved_to_project_and_more, recorded from the Django app this replaced.
ALTER TABLE "pages" ADD COLUMN "moved_to_page" uuid NULL;
ALTER TABLE "pages" ADD COLUMN "moved_to_project" uuid NULL;
ALTER TABLE "page_versions" ADD COLUMN "sub_pages_data" jsonb DEFAULT '{}'::jsonb NOT NULL;
ALTER TABLE "page_versions" ALTER COLUMN "sub_pages_data" DROP DEFAULT;
