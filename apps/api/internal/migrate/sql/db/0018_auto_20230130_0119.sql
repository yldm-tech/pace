-- db.0018_auto_20230130_0119, recorded from the Django app this replaced.
ALTER TABLE "users" ADD COLUMN "is_bot" boolean DEFAULT false NOT NULL;
ALTER TABLE "users" ALTER COLUMN "is_bot" DROP DEFAULT;
ALTER TABLE "issues" ALTER COLUMN "description" DROP NOT NULL;
ALTER TABLE "issues" ALTER COLUMN "description_html" DROP NOT NULL;
ALTER TABLE "issues" ALTER COLUMN "description_stripped" DROP NOT NULL;
CREATE TABLE "api_tokens" ("created_at" timestamp with time zone NOT NULL, "updated_at" timestamp with time zone NOT NULL, "id" uuid NOT NULL PRIMARY KEY, "token" varchar(255) NOT NULL UNIQUE, "label" varchar(255) NOT NULL, "user_type" smallint NOT NULL CHECK ("user_type" >= 0), "created_by_id" uuid NULL, "updated_by_id" uuid NULL, "user_id" uuid NOT NULL);
ALTER TABLE "api_tokens" ADD CONSTRAINT "api_tokens_created_by_id_441e3d24_fk_users_id" FOREIGN KEY ("created_by_id") REFERENCES "users" ("id") DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE "api_tokens" ADD CONSTRAINT "api_tokens_updated_by_id_bcd544cf_fk_users_id" FOREIGN KEY ("updated_by_id") REFERENCES "users" ("id") DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE "api_tokens" ADD CONSTRAINT "api_tokens_user_id_2db24e1c_fk_users_id" FOREIGN KEY ("user_id") REFERENCES "users" ("id") DEFERRABLE INITIALLY DEFERRED;
CREATE INDEX "api_tokens_token_6211101f_like" ON "api_tokens" ("token" varchar_pattern_ops);
CREATE INDEX "api_tokens_created_by_id_441e3d24" ON "api_tokens" ("created_by_id");
CREATE INDEX "api_tokens_updated_by_id_bcd544cf" ON "api_tokens" ("updated_by_id");
CREATE INDEX "api_tokens_user_id_2db24e1c" ON "api_tokens" ("user_id");
