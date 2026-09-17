-- db.0014_alter_workspacememberinvite_unique_together, recorded from the Django app this replaced.
ALTER TABLE "workspace_member_invites" ADD CONSTRAINT "workspace_member_invites_email_workspace_id_9d830009_uniq" UNIQUE ("email", "workspace_id");
