-- license.0002_rename_version_instance_current_version_and_more, recorded from the Django app this replaced.
ALTER TABLE "instances" ALTER COLUMN "instance_id" TYPE varchar(255);
ALTER TABLE "instances" RENAME COLUMN "version" TO "current_version";
ALTER TABLE "instances" DROP COLUMN "api_key" CASCADE;
ALTER TABLE "instances" ADD COLUMN "domain" text DEFAULT '' NOT NULL;
ALTER TABLE "instances" ALTER COLUMN "domain" DROP DEFAULT;
ALTER TABLE "instances" ADD COLUMN "latest_version" varchar(10) NULL;
ALTER TABLE "instances" ADD COLUMN "product" varchar(50) DEFAULT 'plane-ce' NOT NULL;
ALTER TABLE "instances" ALTER COLUMN "product" DROP DEFAULT;
CREATE TABLE "changelogs" ("created_at" timestamp with time zone NOT NULL, "updated_at" timestamp with time zone NOT NULL, "id" uuid NOT NULL PRIMARY KEY, "title" varchar(100) NOT NULL, "description" text NOT NULL, "version" varchar(100) NOT NULL, "tags" jsonb NOT NULL, "release_date" timestamp with time zone NULL, "is_release_candidate" boolean NOT NULL, "created_by_id" uuid NULL, "updated_by_id" uuid NULL);
ALTER TABLE "changelogs" ADD CONSTRAINT "changelogs_created_by_id_16dd944a_fk_users_id" FOREIGN KEY ("created_by_id") REFERENCES "users" ("id") DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE "changelogs" ADD CONSTRAINT "changelogs_updated_by_id_e0989861_fk_users_id" FOREIGN KEY ("updated_by_id") REFERENCES "users" ("id") DEFERRABLE INITIALLY DEFERRED;
CREATE INDEX "changelogs_created_by_id_16dd944a" ON "changelogs" ("created_by_id");
CREATE INDEX "changelogs_updated_by_id_e0989861" ON "changelogs" ("updated_by_id");
