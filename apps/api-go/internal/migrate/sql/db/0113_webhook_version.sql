-- db.0113_webhook_version, recorded by apps/api-go/tools/generate_migration_sql.py. Do not edit by hand.
ALTER TABLE "webhooks" ADD COLUMN "version" varchar(50) DEFAULT 'v1' NOT NULL;
ALTER TABLE "webhooks" ALTER COLUMN "version" DROP DEFAULT;
ALTER TABLE "profiles" ADD COLUMN "is_navigation_tour_completed" boolean DEFAULT false NOT NULL;
ALTER TABLE "profiles" ALTER COLUMN "is_navigation_tour_completed" DROP DEFAULT;
ALTER TABLE "workspace_user_properties" ADD COLUMN "product_tour" jsonb DEFAULT '{"work_items": false, "cycles": false, "modules": false, "intake": false, "pages": false}'::jsonb NOT NULL;
ALTER TABLE "workspace_user_properties" ALTER COLUMN "product_tour" DROP DEFAULT;
ALTER TABLE "api_tokens" ADD COLUMN "allowed_rate_limit" varchar(255) DEFAULT '60/min' NOT NULL;
ALTER TABLE "api_tokens" ALTER COLUMN "allowed_rate_limit" DROP DEFAULT;
ALTER TABLE "profiles" ADD COLUMN "is_subscribed_to_changelog" boolean DEFAULT false NOT NULL;
ALTER TABLE "profiles" ALTER COLUMN "is_subscribed_to_changelog" DROP DEFAULT;
