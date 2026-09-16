-- license.0003_alter_changelog_title_alter_changelog_version_and_more, recorded from the Django app this replaced.
ALTER TABLE "changelogs" ALTER COLUMN "title" TYPE varchar(255);
ALTER TABLE "changelogs" ALTER COLUMN "version" TYPE varchar(255);
ALTER TABLE "instances" ALTER COLUMN "current_version" TYPE varchar(255);
ALTER TABLE "instances" ALTER COLUMN "latest_version" TYPE varchar(255);
ALTER TABLE "instances" ALTER COLUMN "namespace" TYPE varchar(255);
ALTER TABLE "instances" ALTER COLUMN "product" TYPE varchar(255);
