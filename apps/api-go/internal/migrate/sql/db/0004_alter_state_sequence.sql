-- db.0004_alter_state_sequence, recorded from the Django app this replaced.
ALTER TABLE "state" DROP CONSTRAINT "state_sequence_check";
ALTER TABLE "state" ALTER COLUMN "sequence" TYPE double precision USING "sequence"::double precision;
