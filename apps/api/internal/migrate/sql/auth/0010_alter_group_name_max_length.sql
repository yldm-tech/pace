-- auth.0010_alter_group_name_max_length, recorded from the Django app this replaced.
ALTER TABLE "auth_group" ALTER COLUMN "name" TYPE varchar(150);
