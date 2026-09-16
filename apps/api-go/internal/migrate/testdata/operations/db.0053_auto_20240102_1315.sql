-- Rows for db.0053's three operations to act on.
--
-- workspace_user_properties carves a row out of each member's view_props, so the members here have the keys it reads and one has them holding null, which the ORM cannot tell from a missing key. project_user_properties leaves an issue_properties row alone when no member matches it, so one of the two has no member. issue_view rebuilds each workspace view; its filters come from query_data with an empty object as the default, so one view has the key and one does not.
--
-- The rows the two inserting operations create carry a fresh id and the current time.
--
-- UNSTABLE: workspace_user_properties.id, workspace_user_properties.created_at, workspace_user_properties.updated_at, issue_views.id, issue_views.created_at, issue_views.updated_at

INSERT INTO users (password, id, username, email, first_name, last_name, avatar, date_joined, created_at, updated_at, last_location, created_location, is_superuser, is_managed, is_password_expired, is_active, is_staff, is_email_verified, is_password_autoset, is_onboarded, token, billing_address_country, has_billing_address, user_timezone, last_login_ip, last_logout_ip, last_login_medium, last_login_uagent, is_bot, theme, is_tour_completed, onboarding_step, display_name) VALUES
  ('!', '00000000-0000-4000-8000-000000000001', 'ada',   'ada@example.test',   'Ada',   'L', '', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '', '', false, false, false, true, false, false, false, true, 't1', 'INDIA', false, 'UTC', '', '', '', '', false, '{}', false, '{}', 'ada'),
  ('!', '00000000-0000-4000-8000-000000000002', 'grace', 'grace@example.test', 'Grace', 'H', '', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '', '', false, false, false, true, false, false, false, true, 't2', 'INDIA', false, 'UTC', '', '', '', '', false, '{}', false, '{}', 'grace');

INSERT INTO workspaces (created_at, updated_at, id, name, slug, owner_id, organization_size)
VALUES ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000101', 'Acme', 'acme', '00000000-0000-4000-8000-000000000001', '2-10');

INSERT INTO projects (created_at, updated_at, id, name, description, network, identifier, workspace_id, cycle_view, module_view, issue_views_view, page_view, inbox_view, archive_in, close_in)
VALUES ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000201', 'Apollo', '', 0, 'APO', '00000000-0000-4000-8000-000000000101', false, false, false, false, false, 0, 0);

INSERT INTO workspace_members (created_at, updated_at, id, role, member_id, workspace_id, view_props, default_props, is_active, issue_props) VALUES
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000301', 20, '00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000101', '{"display_filters": {"layout": "kanban"}, "display_properties": {"key": false}}', '{}', true, '{}'),
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000302', 15, '00000000-0000-4000-8000-000000000002', '00000000-0000-4000-8000-000000000101', '{"display_filters": {"layout": "list"}, "display_properties": {}}', '{}', true, '{}');

INSERT INTO project_members (created_at, updated_at, id, role, project_id, workspace_id, view_props, default_props, sort_order, preferences, is_active, member_id) VALUES
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000401', 20, '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101', '{"filters": {"labels": ["bug"]}, "display_filters": {"layout": "calendar"}}', '{}', 1, '{}', true, '00000000-0000-4000-8000-000000000001');

INSERT INTO issue_properties (created_at, updated_at, id, display_properties, project_id, user_id, workspace_id, display_filters, filters) VALUES
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000501', '{"key": true}', '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000101', '{}', '{}'),
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000502', '{"key": true}', '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000002', '00000000-0000-4000-8000-000000000101', '{"kept": true}', '{"kept": true}');

INSERT INTO global_views (created_at, updated_at, id, name, description, query, access, query_data, sort_order, workspace_id, created_by_id, updated_by_id) VALUES
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000601', 'With filters', 'a view',  '{}', 1, '{"filters": {"priority": ["urgent"]}}', 100, '00000000-0000-4000-8000-000000000101', '00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000001'),
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000602', 'No filters',   '',        '{}', 0, '{"other": 1}',                          200, '00000000-0000-4000-8000-000000000101', NULL, NULL);
