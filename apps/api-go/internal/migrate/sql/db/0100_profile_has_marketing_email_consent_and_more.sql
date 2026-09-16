-- db.0100_profile_has_marketing_email_consent_and_more, recorded by apps/api-go/tools/generate_migration_sql.py. Do not edit by hand.
ALTER TABLE "profiles" ADD COLUMN "has_marketing_email_consent" boolean DEFAULT false NOT NULL;
ALTER TABLE "profiles" ALTER COLUMN "has_marketing_email_consent" DROP DEFAULT;
