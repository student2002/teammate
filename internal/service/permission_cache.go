// permission_cache.go 实现代理权限检查结果的 Redis 缓存层。
// 使用 Redis 存储权限检查结果以减少数据库查询次数，
// 缓存命中时直接返回，未命中时回退到数据库查询并缓存结果。
// 缓存 TTL 为 60 秒，权限变更时通过 SCAN 批量删除相关缓存键。
package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const (
	// permCachePrefix 是权限缓存键的前缀，格式为 "agent_perm:{agentID}:{permission}"
	permCachePrefix = "agent_perm:"
	// permCacheTTL 是权限缓存的过期时间，60 秒后自动失效
	permCacheTTL = 60 * time.Second
)

// PermissionCache 使用 Redis 缓存代理权限检查结果。
// 缓存键格式为 "agent_perm:{agentID}:{permission}"，值为 "1"（有权限）或 "0"（无权限）。
type PermissionCache struct {
	rdb *redis.Client
}

// NewPermissionCache 创建一个新的 PermissionCache 实例。
// 如果 rdb 为 nil，所有操作为空操作，检查将直接回退到数据库查询。
func NewPermissionCache(rdb *redis.Client) *PermissionCache {
	return &PermissionCache{rdb: rdb}
}

// HasPermission 先检查 Redis 缓存，缓存未命中时通过提供的回退函数查询数据库。
//
// 步骤：
//  1. 若 Redis 客户端为空，直接调用回退函数查询数据库
//  2. 根据 agentID 和 permission 构建缓存键，查询 Redis
//  3. 缓存命中：直接返回缓存值（"1" 表示有权限，"0" 表示无权限）
//  4. 缓存未命中：调用回退函数查询数据库
//  5. 将查询结果写入 Redis 缓存（TTL 60 秒）
//
// 参数：
//   - ctx: 请求上下文
//   - agentID: 代理 ID
//   - permission: 权限标识符
//   - checkFn: 回退函数，当缓存未命中时调用以查询数据库
//
// 返回：
//   - bool: 代理是否拥有指定权限
//   - error: 可能的错误（Redis 查询失败、回退函数返回错误）
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

// Invalidate 使用 SCAN 迭代器删除指定代理的所有缓存权限条目。
// 当代理的权限被授予或撤销时调用此方法，确保缓存一致性。
//
// 步骤：
//  1. 构建匹配模式 "agent_perm:{agentID}:*"
//  2. 使用 SCAN 迭代器分批扫描匹配的键
//  3. 逐个删除匹配的缓存键
//
// 参数：
//   - ctx: 请求上下文
//   - agentID: 代理 ID，用于定位需要清除的缓存键
//
// 返回：
//   - error: 可能的错误（Redis SCAN 迭代失败）
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
