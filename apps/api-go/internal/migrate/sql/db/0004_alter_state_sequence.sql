-- db.0004_alter_state_sequence, recorded by apps/api-go/tools/generate_migration_sql.py. Do not edit by hand.
ALTER TABLE "state" DROP CONSTRAINT "state_sequence_check";
ALTER TABLE "state" ALTER COLUMN "sequence" TYPE double precision USING "sequence"::double precision;
