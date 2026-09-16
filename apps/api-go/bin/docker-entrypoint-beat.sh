#!/bin/sh
# The Go counterpart of apps/api/bin/docker-entrypoint-beat.sh.
set -e

pace-manage wait_for_db
pace-manage wait_for_migrations

exec pace-beat
