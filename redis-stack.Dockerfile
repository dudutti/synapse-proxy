# Custom Redis Stack image with AOF persistence enabled.
#
# The upstream redis/redis-stack-server wrapper hard-codes
# `appendonly no` + `save ""` which wipes all keys on every restart.
# The proxy reads virtual keys from Redis (not Postgres), so a restart
# kills the entire stack until someone manually re-syncs.
#
# We override the entrypoint with redis-start.sh which:
#   * loads the same modules the upstream wrapper does
#   * sets --appendonly yes + --appendfsync everysec
#   * points --dir at /data (the bind-mounted volume)
#
# See redis-stack-override.conf for the layered config and
# redis-start.sh for the entrypoint script.

FROM redis/redis-stack-server:latest

COPY redis-start.sh /usr/local/bin/redis-start.sh
COPY redis-stack-override.conf /etc/redis-stack-override.conf

RUN chmod +x /usr/local/bin/redis-start.sh

ENTRYPOINT ["/usr/local/bin/redis-start.sh"]
CMD ["redis-server", "/etc/redis-stack-override.conf"]