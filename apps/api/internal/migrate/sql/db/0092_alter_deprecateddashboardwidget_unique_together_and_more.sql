-- db.0092_alter_deprecateddashboardwidget_unique_together_and_more, recorded from the Django app this replaced.
ALTER TABLE "deprecated_dashboard_widgets" DROP CONSTRAINT "dashboard_widgets_widget_id_dashboard_id_d_190c068f_uniq";
SET CONSTRAINTS "dashboard_widgets_created_by_id_b5b3ea75_fk_users_id" IMMEDIATE; ALTER TABLE "deprecated_dashboard_widgets" DROP CONSTRAINT "dashboard_widgets_created_by_id_b5b3ea75_fk_users_id";
ALTER TABLE "deprecated_dashboard_widgets" DROP COLUMN "created_by_id" CASCADE;
SET CONSTRAINTS "dashboard_widgets_dashboard_id_38f1dbbd_fk_dashboards_id" IMMEDIATE; ALTER TABLE "deprecated_dashboard_widgets" DROP CONSTRAINT "dashboard_widgets_dashboard_id_38f1dbbd_fk_dashboards_id";
ALTER TABLE "deprecated_dashboard_widgets" DROP COLUMN "dashboard_id" CASCADE;
SET CONSTRAINTS "dashboard_widgets_updated_by_id_8b33a684_fk_users_id" IMMEDIATE; ALTER TABLE "deprecated_dashboard_widgets" DROP CONSTRAINT "dashboard_widgets_updated_by_id_8b33a684_fk_users_id";
ALTER TABLE "deprecated_dashboard_widgets" DROP COLUMN "updated_by_id" CASCADE;
SET CONSTRAINTS "dashboard_widgets_widget_id_a234fdb0_fk_widgets_id" IMMEDIATE; ALTER TABLE "deprecated_dashboard_widgets" DROP CONSTRAINT "dashboard_widgets_widget_id_a234fdb0_fk_widgets_id";
ALTER TABLE "deprecated_dashboard_widgets" DROP COLUMN "widget_id" CASCADE;
DROP TABLE "deprecated_dashboards" CASCADE;
DROP TABLE "deprecated_dashboard_widgets" CASCADE;
DROP TABLE "deprecated_widgets" CASCADE;
