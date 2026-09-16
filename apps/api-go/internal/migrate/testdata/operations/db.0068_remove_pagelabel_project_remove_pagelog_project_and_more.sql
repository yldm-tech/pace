-- Rows for db.0068_remove_pagelabel_project_remove_pagelog_project_and_more to act on. Skeleton from apps/api-go/tools/seed_migration_operation.py; edit the values that matter.
-- The schema here is the one db.0067_issue_estimate leaves behind.
--
-- migrate_pages gives every page a row saying which project it is in. One page has no creator or updater recorded, which the new row copies as-is.
--
-- UNSTABLE: project_pages.id, project_pages.created_at, project_pages.updated_at

INSERT INTO users (password, id, username, first_name, last_name, avatar, date_joined, created_at, updated_at, last_location, created_location, is_superuser, is_managed, is_password_expired, is_active, is_staff, is_email_verified, is_password_autoset, token, user_timezone, last_login_ip, last_logout_ip, last_login_medium, last_login_uagent, is_bot, display_name)
VALUES ('password', '00000000-0000-4000-8000-000000000001', 'username', 'first_name', 'last_name', 'avatar', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', 'last_location', 'created_location', false, false, false, false, false, false, false, 'token', 'user_timezone', 'last_login_ip', 'last_logout_ip', 'last_login_medium', 'last_login_uagent', false, 'display_name');

INSERT INTO users (password, id, username, first_name, last_name, avatar, date_joined, created_at, updated_at, last_location, created_location, is_superuser, is_managed, is_password_expired, is_active, is_staff, is_email_verified, is_password_autoset, token, user_timezone, last_login_ip, last_logout_ip, last_login_medium, last_login_uagent, is_bot, display_name)
VALUES ('password', '00000000-0000-4000-8000-000000000002', 'second', 'first_name', 'last_name', 'avatar', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', 'last_location', 'created_location', false, false, false, false, false, false, false, 'token', 'user_timezone', 'last_login_ip', 'last_logout_ip', 'last_login_medium', 'last_login_uagent', false, 'second');

INSERT INTO workspaces (created_at, updated_at, id, name, slug, owner_id)
VALUES ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000101', 'name', 'acme', '00000000-0000-4000-8000-000000000001');

INSERT INTO projects (created_at, updated_at, id, name, description, network, identifier, workspace_id, cycle_view, module_view, issue_views_view, page_view, inbox_view, archive_in, close_in, logo_props)
VALUES ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000201', 'Apollo', 'description', 0, 'APO', '00000000-0000-4000-8000-000000000101', false, false, false, false, false, 0, 0, '{}');

INSERT INTO projects (created_at, updated_at, id, name, description, network, identifier, workspace_id, cycle_view, module_view, issue_views_view, page_view, inbox_view, archive_in, close_in, logo_props)
VALUES ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000202', 'Gemini', 'description', 0, 'GEM', '00000000-0000-4000-8000-000000000101', false, false, false, false, false, 0, 0, '{}');

INSERT INTO pages (created_at, updated_at, id, name, description, description_html, access, owned_by_id, project_id, workspace_id, color, is_locked, view_props, logo_props, created_by_id, updated_by_id) VALUES
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000301', 'Notes',  '{}', '', 0, '00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101', '', false, '{}', '{}', '00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000002'),
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000302', 'Ideas',  '{}', '', 0, '00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000202', '00000000-0000-4000-8000-000000000101', '', false, '{}', '{}', NULL, NULL);
