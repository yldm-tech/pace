-- db.0096_user_is_email_valid_user_masked_at, recorded by apps/api-go/tools/generate_migration_sql.py. Do not edit by hand.
ALTER TABLE "users" ADD COLUMN "is_email_valid" boolean DEFAULT false NOT NULL;
ALTER TABLE "users" ALTER COLUMN "is_email_valid" DROP DEFAULT;
ALTER TABLE "users" ADD COLUMN "masked_at" timestamp with time zone NULL;
