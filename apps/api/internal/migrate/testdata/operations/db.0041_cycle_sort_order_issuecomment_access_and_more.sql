-- Rows for db.0041's eight operations to act on.
--
-- generate_display_name takes everything before the first @, so the users cover an ordinary address, one with no @ at all, and one whose local part repeats later in the string. update_assignee_issue_activity then matches an activity's value against those emails and rewrites the last word of the comment, so the activities cover a row with only a new value, one with only an old value, one with both (which neither branch touches), one naming nobody, and a comment with runs of whitespace in it, which Python's split collapses.
--
-- UNSTABLE: cycles.sort_order, modules.sort_order

INSERT INTO users (password, id, username, email, first_name, last_name, avatar, date_joined, created_at, updated_at, last_location, created_location, is_superuser, is_managed, is_password_expired, is_active, is_staff, is_email_verified, is_password_autoset, is_onboarded, token, billing_address_country, has_billing_address, user_timezone, last_login_ip, last_logout_ip, last_login_medium, last_login_uagent, is_bot, theme, is_tour_completed, onboarding_step) VALUES
  ('!', '00000000-0000-4000-8000-000000000001', 'ada',  'ada@example.test',    'Ada',  'L', '', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '', '', false, false, false, true, false, false, false, true, 't1', 'INDIA', false, 'Asia/Kolkata', '', '', '', '', false, '{}', false, '{}'),
  ('!', '00000000-0000-4000-8000-000000000002', 'noat', 'no-at-sign',          'No',   'A', '', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '', '', false, false, false, true, false, false, false, true, 't2', 'INDIA', false, 'Europe/Berlin', '', '', '', '', false, '{}', false, '{}'),
  ('!', '00000000-0000-4000-8000-000000000003', 'twice','grace@a@example.test','Grace','H', '', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '', '', false, false, false, true, false, false, false, true, 't3', 'INDIA', false, 'UTC', '', '', '', '', false, '{}', false, '{}');

INSERT INTO workspaces (created_at, updated_at, id, name, slug, owner_id, organization_size)
VALUES ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000101', 'Acme', 'acme', '00000000-0000-4000-8000-000000000001', '2-10');

INSERT INTO projects (created_at, updated_at, id, name, description, network, identifier, workspace_id, cycle_view, module_view, issue_views_view, page_view, inbox_view, archive_in, close_in)
VALUES ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000201', 'Apollo', '', 0, 'APO', '00000000-0000-4000-8000-000000000101', false, false, false, false, false, 0, 0);

INSERT INTO workspace_members (created_at, updated_at, id, role, member_id, workspace_id, view_props, default_props) VALUES
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000301', 20, '00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000101', '{"properties": {"key": true}}', '{"properties": {"key": false}}'),
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000302', 15, '00000000-0000-4000-8000-000000000002', '00000000-0000-4000-8000-000000000101', '{"properties": {"start_date": false}}', '{"properties": {}}');

INSERT INTO issue_activities (created_at, updated_at, id, verb, comment, attachments, project_id, workspace_id, field, old_value, new_value) VALUES
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000501', 'updated', 'added assignee ada@example.test',     '{}', '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101', 'assignee',  '',  'ada@example.test'),
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000502', 'updated', 'removed  assignee   no-at-sign',      '{}', '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101', 'assignees', 'no-at-sign', ''),
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000503', 'updated', 'swapped one for two',                 '{}', '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101', 'assignees', 'ada@example.test', 'no-at-sign'),
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000504', 'updated', 'added assignee nobody@example.test',   '{}', '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101', 'assignees', NULL, 'nobody@example.test'),
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000505', 'updated', 'set the start date to the start date', '{}', '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101', 'name',      '',  'Renamed');

INSERT INTO cycles (created_at, updated_at, id, name, description, owned_by_id, project_id, workspace_id, view_props) VALUES
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000601', 'Sprint 1', '', '00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101', '{}'),
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000602', 'Sprint 2', '', '00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101', '{}');

INSERT INTO modules (created_at, updated_at, id, name, description, status, project_id, workspace_id, view_props) VALUES
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000701', 'Payments', '', 'planned', '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101', '{}');

INSERT INTO issue_properties (created_at, updated_at, id, properties, project_id, user_id, workspace_id) VALUES
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000801', '{}', '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000101'),
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000802', '{"start_date": false, "key": true}', '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000002', '00000000-0000-4000-8000-000000000101');
