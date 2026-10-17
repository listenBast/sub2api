//go:build unit

package service

import (
	"context"
	"testing"
)

// 计费链路通过接口断言调用失效方法；未注入 APIKeyService（typed nil）时不得 panic。
func TestAPIKeyServiceAuthCacheInvalidationIsNilSafe(t *testing.T) {
	var s *APIKeyService
	var invalidator interface {
		InvalidateAuthCacheByUserID(context.Context, int64)
	} = s
	invalidator.InvalidateAuthCacheByUserID(context.Background(), 1)
	s.InvalidateAuthCacheByKey(context.Background(), "k")
	s.InvalidateAuthCacheByGroupID(context.Background(), 1)
}
