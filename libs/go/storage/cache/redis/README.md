# Redis Cache Adapter

`redis` implements `cache.Store` using `github.com/redis/go-redis/v9`.

All read, write, compare-and-swap, TTL, and delete decisions execute atomically
inside Redis Lua scripts. TTL expiration metadata is derived from Redis server
time, avoiding correctness dependence on application-host clock synchronization.
Values are namespaced with a configurable prefix. The caller owns the lifecycle
of the Redis client.
