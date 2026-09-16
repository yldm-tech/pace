-- db.0007_label_parent, recorded by apps/api-go/tools/generate_migration_sql.py. Do not edit by hand.
ALTER TABLE "label" ADD COLUMN "parent_id" uuid NULL CONSTRAINT "label_parent_id_7a853296_fk_label_id" REFERENCES "label"("id") DEFERRABLE INITIALLY DEFERRED; SET CONSTRAINTS "label_parent_id_7a853296_fk_label_id" IMMEDIATE;
CREATE INDEX "label_parent_id_7a853296" ON "label" ("parent_id");
