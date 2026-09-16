-- Rows for db.0045's two operations to act on.
--
-- update_issue_activity_priority replaces a falsy value with the word none, and `or` treats the empty string as falsy as well as null, so the priority activities cover both of those and one that already carries a value. update_issue_activity_blocked renames only the activities whose field is blocks.

INSERT INTO users (password, id, username, email, first_name, last_name, avatar, date_joined, created_at, updated_at, last_location, created_location, is_superuser, is_managed, is_password_expired, is_active, is_staff, is_email_verified, is_password_autoset, is_onboarded, token, billing_address_country, has_billing_address, user_timezone, last_login_ip, last_logout_ip, last_login_medium, last_login_uagent, is_bot, theme, is_tour_completed, onboarding_step, display_name) VALUES
  ('!', '00000000-0000-4000-8000-000000000001', 'ada',   'ada@example.test',   'Ada',   'L', '', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '', '', false, false, false, true, false, false, false, true, 't1', 'INDIA', false, 'UTC', '', '', '', '', false, '{}', false, '{}', 'ada'),
  ('!', '00000000-0000-4000-8000-000000000002', 'grace', 'grace@example.test', 'Grace', 'H', '', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '', '', false, false, false, true, false, false, false, true, 't2', 'INDIA', false, 'UTC', '', '', '', '', false, '{}', false, '{}', 'grace');

INSERT INTO workspaces (created_at, updated_at, id, name, slug, owner_id, organization_size)
VALUES ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000101', 'Acme', 'acme', '00000000-0000-4000-8000-000000000001', '2-10');

INSERT INTO projects (created_at, updated_at, id, name, description, network, identifier, workspace_id, cycle_view, module_view, issue_views_view, page_view, inbox_view, archive_in, close_in)
VALUES ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000201', 'Apollo', '', 0, 'APO', '00000000-0000-4000-8000-000000000101', false, false, false, false, false, 0, 0);

INSERT INTO issue_activities (created_at, updated_at, id, verb, comment, attachments, project_id, workspace_id, field, old_value, new_value) VALUES
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000501', 'updated', 'set the priority',    '{}', '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101', 'priority', NULL,     ''),
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000502', 'updated', 'raised the priority', '{}', '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101', 'priority', 'low',    'urgent'),
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000503', 'updated', 'cleared it',          '{}', '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101', 'priority', 'urgent', NULL),
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000504', 'updated', 'now blocks',          '{}', '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101', 'blocks',   '',       'APO-2'),
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000505', 'updated', 'left alone',          '{}', '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101', 'blocked_by', '',     'APO-3');
