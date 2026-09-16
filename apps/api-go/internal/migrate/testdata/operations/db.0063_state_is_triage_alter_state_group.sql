-- Rows for db.0063's update_project_state_group to act on.
--
-- It moves only the states that are both in the backlog group and named exactly Triage, so the states here cover that pair, one named Triage in another group (in a second project, since a state name is unique per project), one in backlog under another name, and one whose name differs only in case.

INSERT INTO users (password, id, username, email, first_name, last_name, avatar, date_joined, created_at, updated_at, last_location, created_location, is_superuser, is_managed, is_password_expired, is_active, is_staff, is_email_verified, is_password_autoset, is_onboarded, token, billing_address_country, has_billing_address, user_timezone, last_login_ip, last_logout_ip, last_login_medium, last_login_uagent, is_bot, theme, is_tour_completed, onboarding_step, display_name) VALUES
  ('!', '00000000-0000-4000-8000-000000000001', 'ada',   'ada@example.test',   'Ada',   'L', '', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '', '', false, false, false, true, false, false, false, true, 't1', 'INDIA', false, 'UTC', '', '', '', '', false, '{"primary": "#fff"}', true, '{"workspace_join": true}', 'ada'),
  ('!', '00000000-0000-4000-8000-000000000002', 'grace', 'grace@example.test', 'Grace', 'H', '', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '', '', false, false, false, true, false, false, false, false, 't2', 'INDIA', false, 'UTC', '', '', '', '', false, '{}', false, '{}', 'grace');

INSERT INTO workspaces (created_at, updated_at, id, name, slug, owner_id)
VALUES ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000101', 'Acme', 'acme', '00000000-0000-4000-8000-000000000001');

INSERT INTO projects (created_at, updated_at, id, name, description, network, identifier, workspace_id, cycle_view, module_view, issue_views_view, page_view, inbox_view, archive_in, close_in, logo_props)
 VALUES
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000201', 'Apollo', '', 0, 'APO', '00000000-0000-4000-8000-000000000101', false, false, false, false, false, 0, 0, '{}'),
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000202', 'Gemini', '', 0, 'GEM', '00000000-0000-4000-8000-000000000101', false, false, false, false, false, 0, 0, '{}');

INSERT INTO states (created_at, updated_at, id, name, description, color, slug, project_id, workspace_id, sequence, "group", "default") VALUES
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000301', 'Triage',   '', '#f00', 'triage-1',  '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101', 10, 'backlog',   false),
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000302', 'Triage',   '', '#f00', 'triage-2',  '00000000-0000-4000-8000-000000000202', '00000000-0000-4000-8000-000000000101', 20, 'started',   false),
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000303', 'Backlog',  '', '#f00', 'backlog-1', '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101', 30, 'backlog',   true),
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000304', 'triage',   '', '#f00', 'triage-3',  '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101', 40, 'backlog',   false);
