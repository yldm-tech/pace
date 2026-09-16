-- Rows for db.0049's update_pages to act on.
--
-- A page's blocks are folded into its own HTML in sort_order, and the two kinds of block produce different markup: a block pointing at an issue becomes an embed carrying a fresh transaction id, and one that does not becomes a heading followed by the block's own HTML. The pages here cover both kinds in one page with the orders interleaved, a page with a single block, and a page with none at all, which the operation does not touch.
--
-- The transaction id is written twice and differs between runs: once into the page's HTML, where the thirty-two hex characters are masked so the rest of the markup is still compared, and once into a column of its own, which is left out along with the log row's id and timestamps.
--
-- UNSTABLE: page_logs.id, page_logs.created_at, page_logs.updated_at, page_logs.transaction
-- MASK-HEX32: pages.description_html

INSERT INTO users (password, id, username, email, first_name, last_name, avatar, date_joined, created_at, updated_at, last_location, created_location, is_superuser, is_managed, is_password_expired, is_active, is_staff, is_email_verified, is_password_autoset, is_onboarded, token, billing_address_country, has_billing_address, user_timezone, last_login_ip, last_logout_ip, last_login_medium, last_login_uagent, is_bot, theme, is_tour_completed, onboarding_step, display_name) VALUES
  ('!', '00000000-0000-4000-8000-000000000001', 'ada',   'ada@example.test',   'Ada',   'L', '', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '', '', false, false, false, true, false, false, false, true, 't1', 'INDIA', false, 'UTC', '', '', '', '', false, '{}', false, '{}', 'ada'),
  ('!', '00000000-0000-4000-8000-000000000002', 'grace', 'grace@example.test', 'Grace', 'H', '', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '', '', false, false, false, true, false, false, false, true, 't2', 'INDIA', false, 'UTC', '', '', '', '', false, '{}', false, '{}', 'grace');

INSERT INTO workspaces (created_at, updated_at, id, name, slug, owner_id, organization_size)
VALUES ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000101', 'Acme', 'acme', '00000000-0000-4000-8000-000000000001', '2-10');

INSERT INTO projects (created_at, updated_at, id, name, description, network, identifier, workspace_id, cycle_view, module_view, issue_views_view, page_view, inbox_view, archive_in, close_in)
VALUES ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000201', 'Apollo', '', 0, 'APO', '00000000-0000-4000-8000-000000000101', false, false, false, false, false, 0, 0);

INSERT INTO issues (created_at, updated_at, id, name, description, priority, sequence_id, project_id, workspace_id, description_html, sort_order, is_draft) VALUES
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000301', 'First issue',  '{}', 'none', 1, '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101', '', 100, false),
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000302', 'Second issue', '{}', 'none', 42, '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101', '', 200, false);

INSERT INTO pages (created_at, updated_at, id, name, description, description_html, access, owned_by_id, project_id, workspace_id, color, is_locked) VALUES
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000401', 'Mixed',  '{}', '<p>already here</p>', 0, '00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101', '', false),
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000402', 'Single', '{}', '', 0, '00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101', '', false),
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000403', 'Empty',  '{}', '<p>untouched</p>', 0, '00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101', '', false);

INSERT INTO page_blocks (created_at, updated_at, id, name, description, description_html, page_id, project_id, workspace_id, sort_order, sync, issue_id, created_by_id, updated_by_id) VALUES
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000501', 'Prose block',  '{}', '<p>some prose</p>', '00000000-0000-4000-8000-000000000401', '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101', 20, false, NULL, '00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000001'),
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000502', 'Issue block',  '{}', '<p>ignored</p>',    '00000000-0000-4000-8000-000000000401', '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101', 10, false, '00000000-0000-4000-8000-000000000301', '00000000-0000-4000-8000-000000000001', NULL),
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000503', 'Later issue',  '{}', '',                  '00000000-0000-4000-8000-000000000401', '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101', 30, false, '00000000-0000-4000-8000-000000000302', NULL, NULL),
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000504', 'Only block',   '{}', '<p>only</p>',       '00000000-0000-4000-8000-000000000402', '00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101', 10, false, NULL, '00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000001');
