-- db.0063_state_is_triage_alter_state_group, recorded by apps/api-go/tools/generate_migration_sql.py. Do not edit by hand.
ALTER TABLE "states" ADD COLUMN "is_triage" boolean DEFAULT false NOT NULL;
ALTER TABLE "states" ALTER COLUMN "is_triage" DROP DEFAULT;
-- RUN db.0063_state_is_triage_alter_state_group.update_project_state_group
