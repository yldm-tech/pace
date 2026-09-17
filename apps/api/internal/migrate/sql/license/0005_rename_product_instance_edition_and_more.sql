-- license.0005_rename_product_instance_edition_and_more, recorded from the Django app this replaced.
ALTER TABLE "instances" RENAME COLUMN "product" TO "edition";
ALTER TABLE "instances" DROP COLUMN "license_key" CASCADE;
ALTER TABLE "instances" DROP COLUMN "user_count" CASCADE;
ALTER TABLE "instances" ADD COLUMN "is_test" boolean DEFAULT false NOT NULL;
ALTER TABLE "instances" ALTER COLUMN "is_test" DROP DEFAULT;
