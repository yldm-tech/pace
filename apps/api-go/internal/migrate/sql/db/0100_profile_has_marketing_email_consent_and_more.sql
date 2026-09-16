-- db.0100_profile_has_marketing_email_consent_and_more, recorded from the Django app this replaced.
ALTER TABLE "profiles" ADD COLUMN "has_marketing_email_consent" boolean DEFAULT false NOT NULL;
ALTER TABLE "profiles" ALTER COLUMN "has_marketing_email_consent" DROP DEFAULT;
