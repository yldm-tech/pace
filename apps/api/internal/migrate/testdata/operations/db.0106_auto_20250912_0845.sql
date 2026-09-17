-- Rows for db.0106_auto_20250912_0845 to act on. Skeleton from apps/api/tools/seed_migration_operation.py; edit the values that matter.
-- The schema here is the one db.0105_alter_project_cycle_view_and_more leaves behind.
--
-- set_page_sort_order numbers the pages by name, a hundred apart, so the names here are distinct and deliberately out of insertion order. A tie would be ordered arbitrarily by both sides, which is why there is not one.

INSERT INTO users (password, id, username, first_name, last_name, avatar, date_joined, created_at, updated_at, last_location, created_location, is_superuser, is_managed, is_password_expired, is_active, is_staff, is_email_verified, is_password_autoset, token, user_timezone, last_login_ip, last_logout_ip, last_login_medium, last_login_uagent, is_bot, display_name, is_email_valid)
VALUES ('password', '00000000-0000-4000-8000-000000000001', 'username', 'first_name', 'last_name', 'avatar', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', 'last_location', 'created_location', false, false, false, false, false, false, false, 'token', 'user_timezone', 'last_login_ip', 'last_logout_ip', 'last_login_medium', 'last_login_uagent', false, 'display_name', false);

INSERT INTO users (password, id, username, first_name, last_name, avatar, date_joined, created_at, updated_at, last_location, created_location, is_superuser, is_managed, is_password_expired, is_active, is_staff, is_email_verified, is_password_autoset, token, user_timezone, last_login_ip, last_logout_ip, last_login_medium, last_login_uagent, is_bot, display_name, is_email_valid)
VALUES ('password', '00000000-0000-4000-8000-000000000002', 'second', 'first_name', 'last_name', 'avatar', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', 'last_location', 'created_location', false, false, false, false, false, false, false, 'token', 'user_timezone', 'last_login_ip', 'last_logout_ip', 'last_login_medium', 'last_login_uagent', false, 'second', false);

INSERT INTO workspaces (created_at, updated_at, id, name, slug, owner_id, timezone, background_color)
VALUES ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000101', 'name', 'acme', '00000000-0000-4000-8000-000000000001', 'timezone', 'background_color');

INSERT INTO projects (created_at, updated_at, id, name, description, network, identifier, workspace_id, cycle_view, module_view, issue_views_view, page_view, intake_view, archive_in, close_in, logo_props, is_time_tracking_enabled, is_issue_type_enabled, guest_view_all_features, timezone)
VALUES ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000201', 'Apollo', 'description', 0, 'APO', '00000000-0000-4000-8000-000000000101', false, false, false, false, false, 0, 0, '{}', false, false, false, 'timezone');

INSERT INTO projects (created_at, updated_at, id, name, description, network, identifier, workspace_id, cycle_view, module_view, issue_views_view, page_view, intake_view, archive_in, close_in, logo_props, is_time_tracking_enabled, is_issue_type_enabled, guest_view_all_features, timezone)
VALUES ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000202', 'Gemini', 'description', 0, 'GEM', '00000000-0000-4000-8000-000000000101', false, false, false, false, false, 0, 0, '{}', false, false, false, 'timezone');

INSERT INTO pages (created_at, updated_at, id, name, description, description_html, access, owned_by_id, workspace_id, color, is_locked, view_props, logo_props, is_global, sort_order) VALUES
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000301', 'Charlie', '{}', '', 0, '00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000101', '', false, '{}', '{}', false, 1),
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000302', 'alpha',   '{}', '', 0, '00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000101', '', false, '{}', '{}', false, 2),
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000303', 'Bravo',   '{}', '', 0, '00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000101', '', false, '{}', '{}', false, 3);

