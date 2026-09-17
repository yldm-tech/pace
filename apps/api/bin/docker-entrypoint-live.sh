#!/bin/sh
# The collaborative editor. The Node service it replaces had no entrypoint script and waited for nothing, so neither does this: it talks to the API over HTTP rather than to the database, and the API is what waits.
set -e

exec pace-live
