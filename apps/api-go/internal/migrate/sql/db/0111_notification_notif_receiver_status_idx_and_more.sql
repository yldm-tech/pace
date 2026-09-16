-- db.0111_notification_notif_receiver_status_idx_and_more, recorded from the Django app this replaced.
CREATE INDEX CONCURRENTLY "notif_receiver_status_idx" ON "notifications" ("receiver_id", "workspace_id", "read_at", "created_at");
CREATE INDEX CONCURRENTLY "notif_receiver_entity_idx" ON "notifications" ("receiver_id", "workspace_id", "entity_name", "read_at");
CREATE INDEX CONCURRENTLY "notif_receiver_state_idx" ON "notifications" ("receiver_id", "workspace_id", "snoozed_till", "archived_at");
CREATE INDEX CONCURRENTLY "notif_receiver_sender_idx" ON "notifications" ("receiver_id", "workspace_id", "sender");
CREATE INDEX CONCURRENTLY "notif_entity_lookup_idx" ON "notifications" ("workspace_id", "entity_identifier", "entity_name");
CREATE INDEX CONCURRENTLY "asset_asset_idx" ON "file_assets" ("asset");
