// Copyright 2026 Mindclade. All rights reserved.
// Confidential and proprietary.

package redis

import (
	"context"

	redisapi "github.com/redis/go-redis/v9"
)

// scriptRunner keeps Lua execution injectable for deterministic adapter tests
// while production uses go-redis Script.Run and Cmd.Result.
type scriptRunner interface {
	Run(context.Context, redisapi.Scripter, []string, ...any) (any, error)
}

type redisScriptRunner struct{ script *redisapi.Script }

func (runner redisScriptRunner) Run(ctx context.Context, client redisapi.Scripter, keys []string, arguments ...any) (any, error) {
	return runner.script.Run(ctx, client, keys, arguments...).Result()
}

var defaultGetScript scriptRunner = redisScriptRunner{script: redisapi.NewScript(`
if redis.call('EXISTS', KEYS[1]) == 0 then
  return {'miss'}
end
local values = redis.call('HMGET', KEYS[1], 'value', 'version', 'expires_at')
if not values[1] or not values[2] or not values[3] then
  return {'corrupt'}
end
return {'ok', values[1], values[2], values[3]}
`)}

var defaultSetScript scriptRunner = redisScriptRunner{script: redisapi.NewScript(`
local exists = redis.call('EXISTS', KEYS[1])
local current = 0
if exists == 1 then
  current = tonumber(redis.call('HGET', KEYS[1], 'version') or '0')
  if current <= 0 then
    return {'corrupt'}
  end
end
if ARGV[1] == '1' and exists == 1 then
  return {'exists', tostring(current)}
end
local expected = tonumber(ARGV[2])
if expected > 0 and (exists == 0 or current ~= expected) then
  return {'mismatch', tostring(current)}
end

local ttl_milliseconds = tonumber(ARGV[4])
if not ttl_milliseconds or ttl_milliseconds < 0 then
  return {'invalid_ttl'}
end
local expires_at = 0
if ttl_milliseconds > 0 then
  local server_time = redis.call('TIME')
  local now_milliseconds = tonumber(server_time[1]) * 1000 + math.floor(tonumber(server_time[2]) / 1000)
  expires_at = now_milliseconds + ttl_milliseconds
end

local version = current + 1
redis.call('HSET', KEYS[1], 'value', ARGV[3], 'version', tostring(version), 'expires_at', tostring(expires_at))
if ttl_milliseconds > 0 then
  redis.call('PEXPIRE', KEYS[1], ttl_milliseconds)
else
  redis.call('PERSIST', KEYS[1])
end
return {'ok', tostring(version), tostring(expires_at)}
`)}

var defaultDeleteScript scriptRunner = redisScriptRunner{script: redisapi.NewScript(`
if redis.call('EXISTS', KEYS[1]) == 0 then
  return {'miss'}
end
local current = tonumber(redis.call('HGET', KEYS[1], 'version') or '0')
if current <= 0 then
  return {'corrupt'}
end
local expected = tonumber(ARGV[1])
if expected > 0 and current ~= expected then
  return {'mismatch', tostring(current)}
end
redis.call('DEL', KEYS[1])
return {'ok'}
`)}
