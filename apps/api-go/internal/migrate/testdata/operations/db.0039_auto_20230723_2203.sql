-- Rows for db.0039's three operations to act on.
--
-- rename_field only moves activities whose field is exactly "assignee", so there is one of those, one already plural, and one naming something else. update_workspace_member_props branches on `is None`, which is the SQL null and not the JSON one — so there is a member with a null view_props, one with an object, and one holding the JSON value null, which takes the other branch and ends up nested under "properties". update_project_member_sort_order fills every row with a random number, so its column is compared for being filled rather than for what it holds.
--
-- UNSTABLE: project_members.sort_order

-- A workspace member is unique per (workspace, member) and a project member per (project, member), so each row below needs a member of its own.
INSERT INTO users (password, id, username, first_name, last_name, avatar, date_joined, created_at, updated_at, last_location, created_location, is_superuser, is_managed, is_password_expired, is_active, is_staff, is_email_verified, is_password_autoset, is_onboarded, token, billing_address_country, has_billing_address, user_timezone, last_login_ip, last_logout_ip, last_login_medium, last_login_uagent, is_bot, theme, is_tour_completed, onboarding_step) VALUES
  ('!', '00000000-0000-4000-8000-000000000001', 'one',   'One',   'User', '', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '', '', false, false, false, true, false, false, false, true, 'token-1', 'INDIA', false, 'UTC', '', '', '', '', false, '{}', false, '{}'),
  ('!', '00000000-0000-4000-8000-000000000002', 'two',   'Two',   'User', '', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '', '', false, false, false, true, false, false, false, true, 'token-2', 'INDIA', false, 'UTC', '', '', '', '', false, '{}', false, '{}'),
  ('!', '00000000-0000-4000-8000-000000000003', 'three', 'Three', 'User', '', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '', '', false, false, false, true, false, false, false, true, 'token-3', 'INDIA', false, 'UTC', '', '', '', '', false, '{}', false, '{}');

INSERT INTO workspaces (created_at, updated_at, id, name, slug, owner_id, organization_size)
VALUES ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000101', 'Acme', 'acme', '00000000-0000-4000-8000-000000000001', '2-10');

INSERT INTO projects (created_at, updated_at, id, name, description, network, identifier, workspace_id, cycle_view, module_view, issue_views_view, page_view, inbox_view, archive_in, close_in)
VALUES ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000201', 'Apollo', '', 0, 'APO', '00000000-0000-4000-8000-000000000101', false, false, false, false, false, 0, 0);

INSERT INTO workspace_members (created_at, updated_at, id, role, member_id, workspace_id, view_props) VALUES
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000301', 20, '00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000101', NULL),
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000302', 15, '00000000-0000-4000-8000-000000000002', '00000000-0000-4000-8000-000000000101', '{"assignee": true, "labels": false}'),
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000303', 10, '00000000-0000-4000-8000-000000000003', '00000000-0000-4000-8000-000000000101', 'null');

INSERT INTO project_members (created_at, updated_at, id, role, project_id, workspace_id, view_props, default_props, member_id) VALUES
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000401', 20, '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101', '{}', '{}', '00000000-0000-4000-8000-000000000001'),
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000402', 15, '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101', '{}', '{}', '00000000-0000-4000-8000-000000000002');

INSERT INTO issue_activities (created_at, updated_at, id, verb, comment, attachments, project_id, workspace_id, field) VALUES
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000501', 'updated', '', '{}', '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101', 'assignee'),
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000502', 'updated', '', '{}', '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101', 'assignees'),
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000503', 'updated', '', '{}', '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101', 'priority'),
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000504', 'created', '', '{}', '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101', NULL);
