-- contenttypes.0002_remove_content_type_name, recorded by apps/api-go/tools/generate_migration_sql.py. Do not edit by hand.
ALTER TABLE "django_content_type" ALTER COLUMN "name" DROP NOT NULL;
ALTER TABLE "django_content_type" DROP COLUMN "name" CASCADE;
