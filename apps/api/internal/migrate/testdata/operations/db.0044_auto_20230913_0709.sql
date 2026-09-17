-- Rows for db.0044's four operations to act on.
--
-- The three helpers this migration defines read every value out of the old object with a default, so the props here cover a complete object, one missing "filters" entirely, one whose "filters" is there but empty, and one whose keys are present with null values — which is what shows whether the default replaces a stored null or only a missing key.
--
-- update_cycle_props and update_module_props test for a key called "filter", not "filters", so one cycle and one module carry the misspelled key and the rest do not.

INSERT INTO users (password, id, username, email, first_name, last_name, avatar, date_joined, created_at, updated_at, last_location, created_location, is_superuser, is_managed, is_password_expired, is_active, is_staff, is_email_verified, is_password_autoset, is_onboarded, token, billing_address_country, has_billing_address, user_timezone, last_login_ip, last_logout_ip, last_login_medium, last_login_uagent, is_bot, theme, is_tour_completed, onboarding_step, display_name) VALUES
  ('!', '00000000-0000-4000-8000-000000000001', 'ada',   'ada@example.test',   'Ada',   'L', '', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '', '', false, false, false, true, false, false, false, true, 't1', 'INDIA', false, 'UTC', '', '', '', '', false, '{}', false, '{}', 'ada'),
  ('!', '00000000-0000-4000-8000-000000000002', 'grace', 'grace@example.test', 'Grace', 'H', '', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '', '', false, false, false, true, false, false, false, true, 't2', 'INDIA', false, 'UTC', '', '', '', '', false, '{}', false, '{}', 'grace');

INSERT INTO workspaces (created_at, updated_at, id, name, slug, owner_id, organization_size)
VALUES ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000101', 'Acme', 'acme', '00000000-0000-4000-8000-000000000001', '2-10');

INSERT INTO projects (created_at, updated_at, id, name, description, network, identifier, workspace_id, cycle_view, module_view, issue_views_view, page_view, inbox_view, archive_in, close_in)
VALUES ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000201', 'Apollo', '', 0, 'APO', '00000000-0000-4000-8000-000000000101', false, false, false, false, false, 0, 0);

INSERT INTO workspace_members (created_at, updated_at, id, role, member_id, workspace_id, view_props, default_props) VALUES
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000301', 20, '00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000101', '{"filters": {"priority": ["urgent"], "state": null, "type": "active"}, "groupByProperty": "state", "orderBy": "updated_at", "issueView": "kanban", "showEmptyGroups": false, "showSubIssues": false, "calendarDateRange": "a-range", "properties": {"key": false, "link": true}}', '{}'),
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000302', 15, '00000000-0000-4000-8000-000000000002', '00000000-0000-4000-8000-000000000101', '{"filters": {}}', '{"properties": {"assignee": false}}');

INSERT INTO project_members (created_at, updated_at, id, role, project_id, workspace_id, view_props, default_props, sort_order, preferences, member_id) VALUES
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000401', 20, '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101', '{"filters": {"labels": ["bug"], "type": "active"}, "orderBy": "priority"}', '{}', 1, '{}', '00000000-0000-4000-8000-000000000001'),
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000402', 15, '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101', '{}', '{"showSubIssues": false, "calendarDateRange": ""}', 2, '{}', '00000000-0000-4000-8000-000000000002');

INSERT INTO cycles (created_at, updated_at, id, name, description, owned_by_id, project_id, workspace_id, view_props, sort_order) VALUES
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000501', 'Has filter',  '', '00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101', '{"filter": {"priority": ["urgent"]}, "filters": {"state": ["done"]}, "extra": 1}', 10),
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000502', 'Has filters', '', '00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101', '{"filters": {"state": ["done"]}}', 20);

INSERT INTO modules (created_at, updated_at, id, name, description, status, project_id, workspace_id, view_props, sort_order) VALUES
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000601', 'Has filter',  '', 'planned', '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101', '{"filter": {}, "filters": {"labels": ["bug"]}}', 10),
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000602', 'Plain',       '', 'planned', '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101', '{}', 20);
