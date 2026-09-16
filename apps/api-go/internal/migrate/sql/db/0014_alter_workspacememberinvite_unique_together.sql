-- db.0014_alter_workspacememberinvite_unique_together, recorded by apps/api-go/tools/generate_migration_sql.py. Do not edit by hand.
ALTER TABLE "workspace_member_invites" ADD CONSTRAINT "workspace_member_invites_email_workspace_id_9d830009_uniq" UNIQUE ("email", "workspace_id");
