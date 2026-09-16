-- Rows for db.0118_remove_workspaceuserproperties_product_tour_and_more to act on. Skeleton from apps/api-go/tools/seed_migration_operation.py; edit the values that matter.
-- The schema here is the one db.0117_rename_description_draftissue_description_json_and_more leaves behind.
--
-- migrate_all_the_product_tour_to_true marks everything as already seen, for everyone, so what the rows start with only has to be different from what they end with.
--
-- A profile is created for each user by an earlier migration, so there is nothing to insert for that here; the two workspace members below are what the checklist, tips and explored features are written onto.

INSERT INTO users (password, id, username, first_name, last_name, avatar, date_joined, created_at, updated_at, last_location, created_location, is_superuser, is_managed, is_password_expired, is_active, is_staff, is_email_verified, is_password_autoset, token, user_timezone, last_login_ip, last_logout_ip, last_login_medium, last_login_uagent, is_bot, display_name, is_email_valid, is_password_reset_required)
VALUES ('password', '00000000-0000-4000-8000-000000000001', 'username', 'first_name', 'last_name', 'avatar', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', 'last_location', 'created_location', false, false, false, false, false, false, false, 'token', 'user_timezone', 'last_login_ip', 'last_logout_ip', 'last_login_medium', 'last_login_uagent', false, 'display_name', false, false);

INSERT INTO users (password, id, username, first_name, last_name, avatar, date_joined, created_at, updated_at, last_location, created_location, is_superuser, is_managed, is_password_expired, is_active, is_staff, is_email_verified, is_password_autoset, token, user_timezone, last_login_ip, last_logout_ip, last_login_medium, last_login_uagent, is_bot, display_name, is_email_valid, is_password_reset_required)
VALUES ('password', '00000000-0000-4000-8000-000000000002', 'second', 'first_name', 'last_name', 'avatar', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', 'last_location', 'created_location', false, false, false, false, false, false, false, 'token', 'user_timezone', 'last_login_ip', 'last_logout_ip', 'last_login_medium', 'last_login_uagent', false, 'second', false, false);

INSERT INTO workspaces (created_at, updated_at, id, name, slug, owner_id, timezone, background_color)
VALUES ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000101', 'name', 'acme', '00000000-0000-4000-8000-000000000001', 'timezone', 'background_color');

INSERT INTO projects (created_at, updated_at, id, name, description, network, identifier, workspace_id, cycle_view, module_view, issue_views_view, page_view, intake_view, archive_in, close_in, logo_props, is_time_tracking_enabled, is_issue_type_enabled, guest_view_all_features, timezone)
VALUES ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000201', 'Apollo', 'description', 0, 'APO', '00000000-0000-4000-8000-000000000101', false, false, false, false, false, 0, 0, '{}', false, false, false, 'timezone');

INSERT INTO projects (created_at, updated_at, id, name, description, network, identifier, workspace_id, cycle_view, module_view, issue_views_view, page_view, intake_view, archive_in, close_in, logo_props, is_time_tracking_enabled, is_issue_type_enabled, guest_view_all_features, timezone)
VALUES ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000202', 'Gemini', 'description', 0, 'GEM', '00000000-0000-4000-8000-000000000101', false, false, false, false, false, 0, 0, '{}', false, false, false, 'timezone');

INSERT INTO workspace_members (created_at, updated_at, id, role, member_id, workspace_id, view_props, default_props, issue_props, is_active, explored_features, getting_started_checklist, tips) VALUES
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000301', 20, '00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000101', '{}', '{}', '{}', true, '{}', '{}', '{}'),
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000302', 15, '00000000-0000-4000-8000-000000000002', '00000000-0000-4000-8000-000000000101', '{}', '{}', '{}', true, '{"github_integrated": false}', '{"page_created": false}', '{"mobile_app_download": false}');

