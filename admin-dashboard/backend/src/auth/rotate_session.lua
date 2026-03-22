local old_key = KEYS[1]
local new_key = KEYS[2]
local new_data = ARGV[1]
local new_ttl = tonumber(ARGV[2])
local grace_ttl = tonumber(ARGV[3])

local old_data = redis.call('GET', old_key)
if not old_data then
  return nil
end

redis.call('SET', new_key, new_data, 'EX', new_ttl)
redis.call('EXPIRE', old_key, grace_ttl)

return old_data
