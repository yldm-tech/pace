-- Rows for db.0055's three operations to act on.
--
-- create_widgets inserts a fixed eight regardless of what is here. create_dashboards gives every user one, bots included — there is no filter on it, unlike the notification preferences two migrations later — so one of the users below is a bot. create_dashboard_widgets is then the cross product of the two.
--
-- UNSTABLE: widgets.id, widgets.created_at, widgets.updated_at, dashboards.id, dashboards.created_at, dashboards.updated_at, dashboard_widgets.id, dashboard_widgets.created_at, dashboard_widgets.updated_at, dashboard_widgets.widget_id, dashboard_widgets.dashboard_id
--
-- All three inserting operations are in this migration, so every one of those columns is written by the code under test rather than inherited from the copied starting point.

INSERT INTO users (password, id, username, email, first_name, last_name, avatar, date_joined, created_at, updated_at, last_location, created_location, is_superuser, is_managed, is_password_expired, is_active, is_staff, is_email_verified, is_password_autoset, is_onboarded, token, billing_address_country, has_billing_address, user_timezone, last_login_ip, last_logout_ip, last_login_medium, last_login_uagent, is_bot, theme, is_tour_completed, onboarding_step, display_name) VALUES
  ('!', '00000000-0000-4000-8000-000000000001', 'ada',   'ada@example.test',   'Ada',   'L', '', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '', '', false, false, false, true, false, false, false, true, 't1', 'INDIA', false, 'UTC', '', '', '', '', false, '{}', false, '{}', 'ada'),
  ('!', '00000000-0000-4000-8000-000000000002', 'grace', 'grace@example.test', 'Grace', 'H', '', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '', '', false, false, false, true, false, false, false, true, 't2', 'INDIA', false, 'UTC', '', '', '', '', true, '{}', false, '{}', 'grace');

INSERT INTO workspaces (created_at, updated_at, id, name, slug, owner_id, organization_size)
VALUES ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000101', 'Acme', 'acme', '00000000-0000-4000-8000-000000000001', '2-10');

INSERT INTO projects (created_at, updated_at, id, name, description, network, identifier, workspace_id, cycle_view, module_view, issue_views_view, page_view, inbox_view, archive_in, close_in)
VALUES ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000201', 'Apollo', '', 0, 'APO', '00000000-0000-4000-8000-000000000101', false, false, false, false, false, 0, 0);
