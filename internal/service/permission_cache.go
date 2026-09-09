// permission_cache.go implements the Redis cache layer for agent permission check results.
// It uses Redis to store permission check results to reduce the number of database queries.
// On a cache hit it returns directly; on a miss it falls back to a database query and caches the result.
// The cache TTL is 60 seconds; on permission changes, related cache keys are batch-deleted via SCAN.
package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const (
	// permCachePrefix is the prefix of permission cache keys, formatted as "agent_perm:{agentID}:{permission}"
	permCachePrefix = "agent_perm:"
	// permCacheTTL is the expiration time of the permission cache, automatically invalidated after 60 seconds
	permCacheTTL = 60 * time.Second
)

// PermissionCache uses Redis to cache agent permission check results.
// The cache key format is "agent_perm:{agentID}:{permission}", and the value is "1" (has permission) or "0" (no permission).
type PermissionCache struct {
	rdb *redis.Client
}

// NewPermissionCache creates a new PermissionCache instance.
// If rdb is nil, all operations are no-ops and checks fall back directly to database queries.
func NewPermissionCache(rdb *redis.Client) *PermissionCache {
	return &PermissionCache{rdb: rdb}
}

// HasPermission checks the Redis cache first; on a cache miss it queries the database via the provided fallback function.
//
// Steps:
//  1. If the Redis client is empty, call the fallback function directly to query the database
//  2. Build the cache key from agentID and permission, and query Redis
//  3. Cache hit: return the cached value directly ("1" means has permission, "0" means no permission)
//  4. Cache miss: call the fallback function to query the database
//  5. Write the query result to the Redis cache (TTL 60 seconds)
//
// Parameters:
//   - ctx: request context
//   - agentID: agent ID
//   - permission: permission identifier
//   - checkFn: fallback function, called on a cache miss to query the database
//
// Returns:
//   - bool: whether the agent has the specified permission
//   - error: possible errors (Redis query failure, fallback function returning an error)
func (c *PermissionCache) HasPermission(ctx context.Context, agentID uuid.UUID, permission string, checkFn func() (bool, error)) (bool, error) {
	if c.rdb == nil {
		return checkFn()
	}
	key := fmt.Sprintf("%s%s:%s", permCachePrefix, agentID, permission)
	val, err := c.rdb.Get(ctx, key).Result()
	if err == nil {
		return val == "1", nil
	}
	result, err := checkFn()
	if err != nil {
		return false, err
	}
	if result {
		c.rdb.Set(ctx, key, "1", permCacheTTL)
	} else {
		c.rdb.Set(ctx, key, "0", permCacheTTL)
	}
	return result, nil
}

// Invalidate uses a SCAN iterator to delete all cached permission entries for the specified agent.
// This is called when an agent's permissions are granted or revoked to ensure cache consistency.
//
// Steps:
//  1. Build the match pattern "agent_perm:{agentID}:*"
//  2. Use a SCAN iterator to scan matching keys in batches
//  3. Delete matching cache keys one by one
//
// Parameters:
//   - ctx: request context
//   - agentID: agent ID, used to locate the cache keys that need to be cleared
//
// Returns:
//   - error: possible errors (Redis SCAN iteration failure)
func (c *PermissionCache) Invalidate(ctx context.Context, agentID uuid.UUID) error {
	if c.rdb == nil {
		return nil
	}
	pattern := fmt.Sprintf("%s%s:*", permCachePrefix, agentID)
	iter := c.rdb.Scan(ctx, 0, pattern, 100).Iterator()
	for iter.Next(ctx) {
		c.rdb.Del(ctx, iter.Val())
	}
	return iter.Err()
}
