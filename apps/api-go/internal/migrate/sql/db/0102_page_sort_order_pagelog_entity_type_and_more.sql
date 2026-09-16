-- db.0102_page_sort_order_pagelog_entity_type_and_more, recorded from the Django app this replaced.
ALTER TABLE "pages" ADD COLUMN "sort_order" double precision DEFAULT 65535.0 NOT NULL;
ALTER TABLE "pages" ALTER COLUMN "sort_order" DROP DEFAULT;
ALTER TABLE "page_logs" ADD COLUMN "entity_type" varchar(30) NULL;
