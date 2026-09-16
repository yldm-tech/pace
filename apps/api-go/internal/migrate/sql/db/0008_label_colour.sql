-- db.0008_label_colour, recorded by apps/api-go/tools/generate_migration_sql.py. Do not edit by hand.
ALTER TABLE "label" ADD COLUMN "colour" varchar(255) DEFAULT '' NOT NULL;
ALTER TABLE "label" ALTER COLUMN "colour" DROP DEFAULT;
