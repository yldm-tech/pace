-- db.0008_label_colour, recorded from the Django app this replaced.
ALTER TABLE "label" ADD COLUMN "colour" varchar(255) DEFAULT '' NOT NULL;
ALTER TABLE "label" ALTER COLUMN "colour" DROP DEFAULT;
