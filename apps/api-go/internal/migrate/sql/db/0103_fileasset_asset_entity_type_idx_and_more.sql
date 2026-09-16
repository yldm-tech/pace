-- db.0103_fileasset_asset_entity_type_idx_and_more, recorded by apps/api-go/tools/generate_migration_sql.py. Do not edit by hand.
CREATE INDEX CONCURRENTLY "asset_entity_type_idx" ON "file_assets" ("entity_type");
CREATE INDEX CONCURRENTLY "asset_entity_identifier_idx" ON "file_assets" ("entity_identifier");
CREATE INDEX CONCURRENTLY "asset_entity_idx" ON "file_assets" ("entity_type", "entity_identifier");
CREATE INDEX CONCURRENTLY "notif_entity_identifier_idx" ON "notifications" ("entity_identifier");
CREATE INDEX CONCURRENTLY "notif_entity_name_idx" ON "notifications" ("entity_name");
CREATE INDEX CONCURRENTLY "notif_read_at_idx" ON "notifications" ("read_at");
CREATE INDEX CONCURRENTLY "notif_entity_idx" ON "notifications" ("receiver_id", "read_at");
CREATE INDEX CONCURRENTLY "pagelog_entity_type_idx" ON "page_logs" ("entity_type");
CREATE INDEX CONCURRENTLY "pagelog_entity_id_idx" ON "page_logs" ("entity_identifier");
CREATE INDEX CONCURRENTLY "pagelog_entity_name_idx" ON "page_logs" ("entity_name");
CREATE INDEX CONCURRENTLY "pagelog_type_id_idx" ON "page_logs" ("entity_type", "entity_identifier");
CREATE INDEX CONCURRENTLY "pagelog_name_id_idx" ON "page_logs" ("entity_name", "entity_identifier");
CREATE INDEX CONCURRENTLY "fav_entity_type_idx" ON "user_favorites" ("entity_type");
CREATE INDEX CONCURRENTLY "fav_entity_identifier_idx" ON "user_favorites" ("entity_identifier");
CREATE INDEX CONCURRENTLY "fav_entity_idx" ON "user_favorites" ("entity_type", "entity_identifier");
