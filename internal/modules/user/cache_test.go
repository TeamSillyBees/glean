package user

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	platformcache "github.com/teamsillybees/glean/internal/platform/cache"
	"github.com/teamsillybees/glean/internal/platform/idgen"
)

// TestUserCacheSupportsConcurrentReads 验证本地缓存可被并发读取且每次反序列化结果完整。
func TestUserCacheSupportsConcurrentReads(t *testing.T) {
	manager := platformcache.NewManager(platformcache.Config{
		AppName:   "user-cache-test",
		LocalTTL:  time.Minute,
		RemoteTTL: time.Minute,
	}, nil)
	cache := NewUserCache(manager)
	want := &User{
		ID:        idgen.New(1001),
		Username:  "alice",
		RoleCodes: []string{"admin", "viewer"},
		IsEnable:  true,
		CreatedBy: idgen.New(9001),
		UpdatedBy: idgen.New(9001),
	}
	require.NoError(t, cache.Set(context.Background(), want))

	const (
		readers = 16
		reads   = 10
	)
	start := make(chan struct{})
	errorsCh := make(chan error, readers)
	var wait sync.WaitGroup
	for reader := 0; reader < readers; reader++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			for index := 0; index < reads; index++ {
				got, found, err := cache.Get(context.Background(), want.ID)
				if err != nil {
					errorsCh <- err
					return
				}
				if !found || got == nil {
					errorsCh <- fmt.Errorf("cached user not found")
					return
				}
				if got.ID != want.ID || got.Username != want.Username || len(got.RoleCodes) != len(want.RoleCodes) {
					errorsCh <- fmt.Errorf("unexpected cached user: %#v", got)
					return
				}
			}
		}()
	}
	close(start)
	wait.Wait()
	close(errorsCh)

	for err := range errorsCh {
		require.NoError(t, err)
	}
}
