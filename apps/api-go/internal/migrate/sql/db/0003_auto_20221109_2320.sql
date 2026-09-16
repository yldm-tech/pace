-- db.0003_auto_20221109_2320, recorded by apps/api-go/tools/generate_migration_sql.py. Do not edit by hand.
SET CONSTRAINTS "issue_property_user_id_0b1d1c8f_fk_user_id" IMMEDIATE; ALTER TABLE "issue_property" DROP CONSTRAINT "issue_property_user_id_0b1d1c8f_fk_user_id";
ALTER TABLE "issue_property" DROP CONSTRAINT "issue_property_user_id_key";
CREATE INDEX "issue_property_user_id_0b1d1c8f" ON "issue_property" ("user_id");
ALTER TABLE "issue_property" ADD CONSTRAINT "issue_property_user_id_0b1d1c8f_fk_user_id" FOREIGN KEY ("user_id") REFERENCES "user" ("id") DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE "issue_property" ADD CONSTRAINT "issue_property_user_id_project_id_fe52668f_uniq" UNIQUE ("user_id", "project_id");
