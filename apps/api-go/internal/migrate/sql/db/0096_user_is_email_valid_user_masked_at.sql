-- db.0096_user_is_email_valid_user_masked_at, recorded from the Django app this replaced.
ALTER TABLE "users" ADD COLUMN "is_email_valid" boolean DEFAULT false NOT NULL;
ALTER TABLE "users" ALTER COLUMN "is_email_valid" DROP DEFAULT;
ALTER TABLE "users" ADD COLUMN "masked_at" timestamp with time zone NULL;
