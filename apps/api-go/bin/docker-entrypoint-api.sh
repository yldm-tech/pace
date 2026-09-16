#!/bin/sh
# The Go counterpart of apps/api/bin/docker-entrypoint-api.sh, step for step.
#
# Starting the server is the last thing this does and the least of what it does. Everything above it is what the Django entrypoint ran on every boot, and dropping it is not visible until an instance is unregistered or the bucket is missing.
set -e

pace-manage wait_for_db
pace-manage wait_for_migrations

# register_instance wants a machine signature, and upstream only checks that it is not empty: the Django command reads it, raises if it is blank, and never writes it down. The shell script it came from hashed the hostname, the mac address, /proc/cpuinfo, free and df together, which produces a different string on a different base image anyway. What is kept is the part that matters — a value derived from this machine, and never an empty one.
MACHINE_SIGNATURE="$(hostname)-$(cat /etc/machine-id 2>/dev/null || echo no-machine-id)"
export MACHINE_SIGNATURE

pace-manage register_instance "$MACHINE_SIGNATURE"
pace-manage configure_instance
pace-manage create_bucket
pace-manage clear_cache

# There is no collectstatic here. Django ran it to lay out the files whitenoise serves under /static/; the Go service has none, and the proxy's /static/ route answers from it the same way Django answered for a file that was not there.
exec pace-api
