-- db.0054_dashboard_widget_dashboardwidget, recorded from the Django app this replaced.
CREATE TABLE "dashboards" ("created_at" timestamp with time zone NOT NULL, "updated_at" timestamp with time zone NOT NULL, "id" uuid NOT NULL PRIMARY KEY, "name" varchar(255) NOT NULL, "description_html" text NOT NULL, "identifier" uuid NULL, "is_default" boolean NOT NULL, "type_identifier" varchar(30) NOT NULL, "created_by_id" uuid NULL, "owned_by_id" uuid NOT NULL, "updated_by_id" uuid NULL);
CREATE TABLE "widgets" ("id" uuid NOT NULL PRIMARY KEY, "created_at" timestamp with time zone NOT NULL, "updated_at" timestamp with time zone NOT NULL, "key" varchar(255) NOT NULL, "filters" jsonb NOT NULL);
CREATE TABLE "dashboard_widgets" ("created_at" timestamp with time zone NOT NULL, "updated_at" timestamp with time zone NOT NULL, "id" uuid NOT NULL PRIMARY KEY, "is_visible" boolean NOT NULL, "sort_order" double precision NOT NULL, "filters" jsonb NOT NULL, "properties" jsonb NOT NULL, "created_by_id" uuid NULL, "dashboard_id" uuid NOT NULL, "updated_by_id" uuid NULL, "widget_id" uuid NOT NULL);
ALTER TABLE "dashboards" ADD CONSTRAINT "dashboards_created_by_id_9cb09793_fk_users_id" FOREIGN KEY ("created_by_id") REFERENCES "users" ("id") DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE "dashboards" ADD CONSTRAINT "dashboards_owned_by_id_29126465_fk_users_id" FOREIGN KEY ("owned_by_id") REFERENCES "users" ("id") DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE "dashboards" ADD CONSTRAINT "dashboards_updated_by_id_11a56f0d_fk_users_id" FOREIGN KEY ("updated_by_id") REFERENCES "users" ("id") DEFERRABLE INITIALLY DEFERRED;
CREATE INDEX "dashboards_created_by_id_9cb09793" ON "dashboards" ("created_by_id");
CREATE INDEX "dashboards_owned_by_id_29126465" ON "dashboards" ("owned_by_id");
CREATE INDEX "dashboards_updated_by_id_11a56f0d" ON "dashboards" ("updated_by_id");
ALTER TABLE "dashboard_widgets" ADD CONSTRAINT "dashboard_widgets_widget_id_dashboard_id_149a0e15_uniq" UNIQUE ("widget_id", "dashboard_id");
ALTER TABLE "dashboard_widgets" ADD CONSTRAINT "dashboard_widgets_created_by_id_b5b3ea75_fk_users_id" FOREIGN KEY ("created_by_id") REFERENCES "users" ("id") DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE "dashboard_widgets" ADD CONSTRAINT "dashboard_widgets_dashboard_id_38f1dbbd_fk_dashboards_id" FOREIGN KEY ("dashboard_id") REFERENCES "dashboards" ("id") DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE "dashboard_widgets" ADD CONSTRAINT "dashboard_widgets_updated_by_id_8b33a684_fk_users_id" FOREIGN KEY ("updated_by_id") REFERENCES "users" ("id") DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE "dashboard_widgets" ADD CONSTRAINT "dashboard_widgets_widget_id_a234fdb0_fk_widgets_id" FOREIGN KEY ("widget_id") REFERENCES "widgets" ("id") DEFERRABLE INITIALLY DEFERRED;
CREATE INDEX "dashboard_widgets_created_by_id_b5b3ea75" ON "dashboard_widgets" ("created_by_id");
CREATE INDEX "dashboard_widgets_dashboard_id_38f1dbbd" ON "dashboard_widgets" ("dashboard_id");
CREATE INDEX "dashboard_widgets_updated_by_id_8b33a684" ON "dashboard_widgets" ("updated_by_id");
CREATE INDEX "dashboard_widgets_widget_id_a234fdb0" ON "dashboard_widgets" ("widget_id");
