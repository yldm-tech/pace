#!/bin/sh
# The Go counterpart of apps/api/bin/docker-entrypoint-migrator.sh.
#
# It does not wait for migrations, because it is the thing that applies them.
set -e

pace-manage wait_for_db
exec pace-manage migrate
