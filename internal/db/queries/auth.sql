-- 认证令牌查询

-- name: GetAuthTokenByLookupHashAndType :one
SELECT owner_type, owner_id, token_hash FROM auth_tokens
WHERE lookup_hash = $1 AND token_type = $2 AND expires_at > NOW();
