-- db.0035_auto_20230704_2225, recorded by apps/api-go/tools/generate_migration_sql.py. Do not edit by hand.
ALTER TABLE "workspaces" ADD COLUMN "organization_size" varchar(20) DEFAULT '2-10' NOT NULL;
ALTER TABLE "workspaces" ALTER COLUMN "organization_size" DROP DEFAULT;
-- RUN db.0035_auto_20230704_2225.update_company_organization_size
ALTER TABLE "workspaces" ALTER COLUMN "name" TYPE varchar(80);
ALTER TABLE "workspaces" ALTER COLUMN "slug" TYPE varchar(48);
ALTER TABLE "workspaces" DROP COLUMN "company_size" CASCADE;
