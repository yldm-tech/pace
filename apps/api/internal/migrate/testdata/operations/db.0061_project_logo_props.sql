-- Rows for db.0061's update_project_logo_props to act on.
--
-- in_use is decided by whether the project has an emoji, and the empty string counts as not having one. icon_prop is read only when it is truthy, and an empty object is falsy in Python, so the projects here cover a null icon_prop, an empty one, a complete one, one missing a key and one holding that key as null. logo_props is added by this same migration with a default of {}, so every project starts from the same empty object and the concatenation has nothing of its own to preserve.

INSERT INTO users (password, id, username, email, first_name, last_name, avatar, date_joined, created_at, updated_at, last_location, created_location, is_superuser, is_managed, is_password_expired, is_active, is_staff, is_email_verified, is_password_autoset, is_onboarded, token, billing_address_country, has_billing_address, user_timezone, last_login_ip, last_logout_ip, last_login_medium, last_login_uagent, is_bot, theme, is_tour_completed, onboarding_step, display_name) VALUES
  ('!', '00000000-0000-4000-8000-000000000001', 'ada',   'ada@example.test',   'Ada',   'L', '', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '', '', false, false, false, true, false, false, false, true, 't1', 'INDIA', false, 'UTC', '', '', '', '', false, '{"primary": "#fff"}', true, '{"workspace_join": true}', 'ada'),
  ('!', '00000000-0000-4000-8000-000000000002', 'grace', 'grace@example.test', 'Grace', 'H', '', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '', '', false, false, false, true, false, false, false, false, 't2', 'INDIA', false, 'UTC', '', '', '', '', false, '{}', false, '{}', 'grace');

INSERT INTO workspaces (created_at, updated_at, id, name, slug, owner_id)
VALUES ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000101', 'Acme', 'acme', '00000000-0000-4000-8000-000000000001');

INSERT INTO projects (created_at, updated_at, id, name, description, network, identifier, workspace_id, cycle_view, module_view, issue_views_view, page_view, inbox_view, archive_in, close_in, emoji, icon_prop) VALUES
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000201', 'Emoji',      '', 0, 'EMO', '00000000-0000-4000-8000-000000000101', false, false, false, false, false, 0, 0, '128640', NULL),
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000202', 'Empty icon', '', 0, 'EMP', '00000000-0000-4000-8000-000000000101', false, false, false, false, false, 0, 0, '', '{}'),
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000203', 'Full icon',  '', 0, 'FUL', '00000000-0000-4000-8000-000000000101', false, false, false, false, false, 0, 0, NULL, '{"name": "Briefcase", "color": "#ff0000"}'),
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000204', 'Part icon',  '', 0, 'PAR', '00000000-0000-4000-8000-000000000101', false, false, false, false, false, 0, 0, NULL, '{"name": "Rocket"}'),
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000205', 'Null name',  '', 0, 'NUL', '00000000-0000-4000-8000-000000000101', false, false, false, false, false, 0, 0, NULL, '{"name": null, "color": "#00ff00"}');
