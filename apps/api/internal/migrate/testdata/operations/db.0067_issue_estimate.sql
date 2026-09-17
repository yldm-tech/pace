-- Rows for db.0067_issue_estimate to act on. Skeleton from apps/api/tools/seed_migration_operation.py; edit the values that matter.
-- The schema here is the one db.0066_account_id_token_cycle_logo_props_module_logo_props leaves behind.
--
-- issue_estimate_point looks an estimate key up among the points of the project's own estimate, so one project has an estimate and one does not. The activities cover a new value with a match, an old value with a match, and one with neither side set; the issues cover a point that matches, a point that does not, and no point at all.
--
-- The integer column is called estimate_point here and is renamed to point by this same migration, before the operation runs — which is why the seed names one and the Go names the other.
--
-- populate_deploy_board copies each published board over. It passes the old board's created_at and updated_at, and they are ignored: both fields are auto_now_add and auto_now, which override whatever is supplied. The old board below carries timestamps from another year so that shows.
--
-- UNSTABLE: deploy_boards.id, deploy_boards.anchor, deploy_boards.created_at, deploy_boards.updated_at

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

INSERT INTO estimates (created_at, updated_at, id, name, description, project_id, workspace_id, type) VALUES
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000301', 'Points', '', '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101', 'points');

INSERT INTO estimate_points (created_at, updated_at, id, key, description, value, estimate_id, project_id, workspace_id) VALUES
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000401', 1, '', 'one', '00000000-0000-4000-8000-000000000301', '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101'),
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000402', 2, '', 'two', '00000000-0000-4000-8000-000000000301', '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101');

INSERT INTO issues (created_at, updated_at, id, name, description, priority, sequence_id, project_id, workspace_id, description_html, sort_order, is_draft, estimate_point) VALUES
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000501', 'Has point',    '{}', 'none', 1, '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101', '', 100, false, 2),
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000502', 'Unknown point','{}', 'none', 2, '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101', '', 200, false, 9),
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000503', 'No point',     '{}', 'none', 3, '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101', '', 300, false, NULL),
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000504', 'Other project','{}', 'none', 1, '00000000-0000-4000-8000-000000000202', '00000000-0000-4000-8000-000000000101', '', 400, false, 1);

INSERT INTO issue_activities (created_at, updated_at, id, verb, comment, attachments, project_id, workspace_id, field, old_value, new_value) VALUES
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000601', 'updated', 'set the estimate',     '{}', '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101', 'estimate_point', '',  '1'),
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000602', 'updated', 'cleared the estimate', '{}', '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101', 'estimate_point', '2', ''),
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000603', 'updated', 'neither side',         '{}', '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101', 'estimate_point', '',  ''),
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000604', 'updated', 'a different field',    '{}', '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101', 'priority',       '1', '2');

INSERT INTO project_deploy_boards (created_at, updated_at, id, anchor, comments, reactions, votes, views, project_id, workspace_id) VALUES
  ('2023-06-01 00:00:00+00', '2023-07-01 00:00:00+00', '00000000-0000-4000-8000-000000000701', 'old-anchor', true, false, true, '{"list": true}', '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101');

UPDATE projects SET estimate_id = '00000000-0000-4000-8000-000000000301' WHERE id = '00000000-0000-4000-8000-000000000201';
