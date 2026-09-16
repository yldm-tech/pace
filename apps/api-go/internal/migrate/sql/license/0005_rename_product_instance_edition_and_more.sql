-- license.0005_rename_product_instance_edition_and_more, recorded by apps/api-go/tools/generate_migration_sql.py. Do not edit by hand.
ALTER TABLE "instances" RENAME COLUMN "product" TO "edition";
ALTER TABLE "instances" DROP COLUMN "license_key" CASCADE;
ALTER TABLE "instances" DROP COLUMN "user_count" CASCADE;
ALTER TABLE "instances" ADD COLUMN "is_test" boolean DEFAULT false NOT NULL;
ALTER TABLE "instances" ALTER COLUMN "is_test" DROP DEFAULT;
