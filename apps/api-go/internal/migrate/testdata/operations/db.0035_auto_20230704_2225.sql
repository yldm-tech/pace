-- Rows for db.0035's update_company_organization_size to act on.
--
-- Every timestamp here is a literal. The two databases are seeded by separate runs of psql, so a now() would differ between them by however long the first migration took and every row would come out mismatched for a reason that has nothing to do with the operation.
--
-- The operation reads company_size and writes its decimal text into organization_size, so the sizes here are the ones worth disagreeing about: the field's own default, zero, and a number wider than one digit.
INSERT INTO users (password, id, username, first_name, last_name, avatar, date_joined, created_at, updated_at, last_location, created_location, is_superuser, is_managed, is_password_expired, is_active, is_staff, is_email_verified, is_password_autoset, is_onboarded, token, billing_address_country, has_billing_address, user_timezone, last_login_ip, last_logout_ip, last_login_medium, last_login_uagent, is_bot, theme)
VALUES ('!', '00000000-0000-4000-8000-000000000001', 'ada', 'Ada', 'Lovelace', '', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '', '', false, false, false, true, false, false, false, true, 'token', 'INDIA', false, 'UTC', '', '', '', '', false, '{}');

INSERT INTO workspaces (created_at, updated_at, id, name, slug, owner_id, company_size) VALUES
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000101', 'Default', 'default', '00000000-0000-4000-8000-000000000001', 10),
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000102', 'Zero',    'zero',    '00000000-0000-4000-8000-000000000001', 0),
  ('2024-01-01 00:00:00+00', '2024-01-01 00:00:00+00', '00000000-0000-4000-8000-000000000103', 'Wide',    'wide',    '00000000-0000-4000-8000-000000000001', 12345);
