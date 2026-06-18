#!/bin/bash
# Custom entrypoint for redis-stack-server with persistence.
#
# The default /usr/bin/redis-stack-server wrapper forces:
#   dir /var/lib/redis-stack
#   save ""           (no snapshot persistence)
#   appendonly no     (no AOF persistence)
#
# Without overriding these, Redis wipes all keys on every container
# restart. The proxy reads virtual keys from Redis (not Postgres), so
# a restart breaks the proxy entirely until someone manually re-syncs.
# We hit this in production on 2026-06-17.
#
# This script invokes redis-server directly with the same modules the
# redis-stack wrapper would load, plus --dir /data (the bind-mounted
# volume) and --appendonly yes for AOF durability.

set -e

MODULEDIR=/opt/redis-stack/lib

exec redis-server \
  --port 6379 \
  --daemonize no \
  --protected-mode no \
  --dir /data \
  --appendonly yes \
  --appendfsync everysec \
  --loadmodule ${MODULEDIR}/rediscompat.so \
  --loadmodule ${MODULEDIR}/redisearch.so MAXSEARCHRESULTS 10000 MAXAGGREGATERESULTS 10000 \
  --loadmodule ${MODULEDIR}/redistimeseries.so \
  --loadmodule ${MODULEDIR}/rejson.so \
  --loadmodule ${MODULEDIR}/redisbloom.so \
  --loadmodule ${MODULEDIR}/redisgears.so v8-plugin-path ${MODULEDIR}/libredisgears_v8_plugin.so \
  "$@"
