-- Rows for db.0065's two operations to act on.
--
-- migrate_user_profile gives every user a profile carved out of their own columns. It selects onboarding_step and role and then does not pass either, so the profile's onboarding_step is the model's default rather than the user's; one of the users below carries a non-default onboarding_step so that shows.
--
-- user_favorite_migration folds five favourite tables into one, so there is a row in each. The project one is the odd case: its entity is the project itself, so the identifier and the project column come out the same.
--
-- Both operations create rows, which carry a fresh id and the current time.
--
-- UNSTABLE: profiles.id, profiles.created_at, profiles.updated_at, user_favorites.id, user_favorites.created_at, user_favorites.updated_at

INSERT INTO users (password, id, username, email, first_name, last_name, avatar, date_joined, created_at, updated_at, last_location, created_location, is_superuser, is_managed, is_password_expired, is_active, is_staff, is_email_verified, is_password_autoset, is_onboarded, token, billing_address_country, has_billing_address, user_timezone, last_login_ip, last_logout_ip, last_login_medium, last_login_uagent, is_bot, theme, is_tour_completed, onboarding_step, display_name) VALUES
  ('!', '00000000-0000-4000-8000-000000000001', 'ada',   'ada@example.test',   'Ada',   'L', '', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '', '', false, false, false, true, false, false, false, true, 't1', 'INDIA', false, 'UTC', '', '', '', '', false, '{"primary": "#fff"}', true, '{"workspace_join": true}', 'ada'),
  ('!', '00000000-0000-4000-8000-000000000002', 'grace', 'grace@example.test', 'Grace', 'H', '', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '', '', false, false, false, true, false, false, false, false, 't2', 'INDIA', false, 'UTC', '', '', '', '', false, '{}', false, '{}', 'grace');

INSERT INTO workspaces (created_at, updated_at, id, name, slug, owner_id)
VALUES ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000101', 'Acme', 'acme', '00000000-0000-4000-8000-000000000001');

INSERT INTO projects (created_at, updated_at, id, name, description, network, identifier, workspace_id, cycle_view, module_view, issue_views_view, page_view, inbox_view, archive_in, close_in, logo_props)
 VALUES
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000201', 'Apollo', '', 0, 'APO', '00000000-0000-4000-8000-000000000101', false, false, false, false, false, 0, 0, '{}'),
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000202', 'Gemini', '', 0, 'GEM', '00000000-0000-4000-8000-000000000101', false, false, false, false, false, 0, 0, '{}');

INSERT INTO cycles (created_at, updated_at, id, name, description, owned_by_id, project_id, workspace_id, view_props, sort_order, progress_snapshot)
VALUES ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000301', 'Sprint 1', '', '00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101', '{}', 10, '{}');

INSERT INTO modules (created_at, updated_at, id, name, description, status, project_id, workspace_id, view_props, sort_order)
VALUES ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000401', 'Payments', '', 'planned', '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101', '{}', 10);

INSERT INTO pages (created_at, updated_at, id, name, description, description_html, access, owned_by_id, project_id, workspace_id, color, is_locked, view_props)
VALUES ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000501', 'Notes', '{}', '', 0, '00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101', '', false, '{}');

INSERT INTO issue_views (created_at, updated_at, id, name, description, query, access, filters, workspace_id, display_filters, display_properties, sort_order, project_id)
VALUES ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000601', 'My view', '', '{}', 1, '{}', '00000000-0000-4000-8000-000000000101', '{}', '{}', 65535, '00000000-0000-4000-8000-000000000201');

INSERT INTO cycle_favorites (created_at, updated_at, id, cycle_id, project_id, user_id, workspace_id, created_by_id, updated_by_id)
VALUES ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000701', '00000000-0000-4000-8000-000000000301', '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000101', '00000000-0000-4000-8000-000000000001', NULL);

INSERT INTO module_favorites (created_at, updated_at, id, module_id, project_id, user_id, workspace_id, created_by_id, updated_by_id)
VALUES ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000702', '00000000-0000-4000-8000-000000000401', '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000101', NULL, '00000000-0000-4000-8000-000000000001');

INSERT INTO project_favorites (created_at, updated_at, id, project_id, user_id, workspace_id, created_by_id, updated_by_id)
VALUES ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000703', '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000002', '00000000-0000-4000-8000-000000000101', NULL, NULL);

INSERT INTO page_favorites (created_at, updated_at, id, page_id, project_id, user_id, workspace_id, created_by_id, updated_by_id)
VALUES ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000704', '00000000-0000-4000-8000-000000000501', '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000101', '00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000001');

INSERT INTO view_favorites (created_at, updated_at, id, project_id, user_id, view_id, workspace_id, created_by_id, updated_by_id)
VALUES ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000705', '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000002', '00000000-0000-4000-8000-000000000601', '00000000-0000-4000-8000-000000000101', NULL, NULL);
