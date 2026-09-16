-- db.0063_state_is_triage_alter_state_group, recorded from the Django app this replaced.
ALTER TABLE "states" ADD COLUMN "is_triage" boolean DEFAULT false NOT NULL;
ALTER TABLE "states" ALTER COLUMN "is_triage" DROP DEFAULT;
-- RUN db.0063_state_is_triage_alter_state_group.update_project_state_group
