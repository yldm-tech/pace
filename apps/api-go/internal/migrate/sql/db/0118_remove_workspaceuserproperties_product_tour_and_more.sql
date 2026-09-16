-- db.0118_remove_workspaceuserproperties_product_tour_and_more, recorded by apps/api-go/tools/generate_migration_sql.py. Do not edit by hand.
ALTER TABLE "workspace_user_properties" DROP COLUMN "product_tour" CASCADE;
ALTER TABLE "profiles" ADD COLUMN "product_tour" jsonb DEFAULT '{"work_items": false, "cycles": false, "modules": false, "intake": false, "pages": false}'::jsonb NOT NULL;
ALTER TABLE "profiles" ALTER COLUMN "product_tour" DROP DEFAULT;
-- RUN db.0118_remove_workspaceuserproperties_product_tour_and_more.migrate_all_the_product_tour_to_true
