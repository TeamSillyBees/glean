package redisx

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type codecUser struct {
	ID   int64  `json:"id" msgpack:"id"`
	Name string `json:"name" msgpack:"name"`
}

func TestCodecsRoundTrip(t *testing.T) {
	codecs := []Codec{JSONCodec{}, MsgpackCodec{}}
	for _, codec := range codecs {
		t.Run(codec.Name(), func(t *testing.T) {
			input := codecUser{ID: 1, Name: "Alice"}

			payload, err := codec.Marshal(input)
			require.NoError(t, err)

			var output codecUser
			require.NoError(t, codec.Unmarshal(payload, &output))
			require.Equal(t, input, output)
		})
	}
}

func TestCacheSetGetDelete(t *testing.T) {
	_, client := newRedisForTest(t)
	ctx := context.Background()

	cache, err := NewCache[codecUser](CacheOptions{
		Client: client,
		Codec:  JSONCodec{},
		TTL:    time.Minute,
		Jitter: time.Minute,
	})
	require.NoError(t, err)

	require.NoError(t, cache.Set(ctx, "user:1", codecUser{ID: 1, Name: "Alice"}))
	got, ok, err := cache.Get(ctx, "user:1")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, codecUser{ID: 1, Name: "Alice"}, got)

	ttl := client.TTL(ctx, "user:1").Val()
	require.GreaterOrEqual(t, ttl, time.Minute)
	require.LessOrEqual(t, ttl, 2*time.Minute)

	require.NoError(t, cache.Delete(ctx, "user:1"))
	_, ok, err = cache.Get(ctx, "user:1")
	require.NoError(t, err)
	require.False(t, ok)
}

func TestCacheGetOrLoadCachesEmptyValue(t *testing.T) {
	_, client := newRedisForTest(t)
	ctx := context.Background()

	cache, err := NewCache[string](CacheOptions{
		Client:   client,
		Codec:    JSONCodec{},
		TTL:      time.Minute,
		NullTTL:  time.Minute,
		CacheNil: true,
	})
	require.NoError(t, err)

	var loads int32
	loader := func(context.Context) (string, bool, error) {
		atomic.AddInt32(&loads, 1)
		return "", false, nil
	}

	value, ok, err := cache.GetOrLoad(ctx, "missing-user", loader)
	require.NoError(t, err)
	require.False(t, ok)
	require.Empty(t, value)

	value, ok, err = cache.GetOrLoad(ctx, "missing-user", loader)
	require.NoError(t, err)
	require.False(t, ok)
	require.Empty(t, value)
	require.EqualValues(t, 1, atomic.LoadInt32(&loads))
}

func TestCacheGetOrLoadUsesSingleflight(t *testing.T) {
	_, client := newRedisForTest(t)
	ctx := context.Background()

	cache, err := NewCache[string](CacheOptions{
		Client: client,
		Codec:  JSONCodec{},
		TTL:    time.Minute,
	})
	require.NoError(t, err)

	var loads int32
	loaderStarted := make(chan struct{})
	releaseLoader := make(chan struct{})
	loader := func(context.Context) (string, bool, error) {
		if atomic.AddInt32(&loads, 1) == 1 {
			close(loaderStarted)
		}
		<-releaseLoader
		return "loaded", true, nil
	}

	const goroutines = 8
	var wg sync.WaitGroup
	errs := make(chan error, goroutines)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, ok, err := cache.GetOrLoad(ctx, "shared", loader)
			if err != nil {
				errs <- err
				return
			}
			if !ok || got != "loaded" {
				errs <- errors.New("unexpected loaded value")
			}
		}()
	}

	<-loaderStarted
	close(releaseLoader)
	wg.Wait()
	close(errs)
	require.Empty(t, errs)
	require.EqualValues(t, 1, atomic.LoadInt32(&loads))
}

func TestCacheCleansCorruptPayload(t *testing.T) {
	_, client := newRedisForTest(t)
	ctx := context.Background()

	cache, err := NewCache[codecUser](CacheOptions{
		Client: client,
		Codec:  JSONCodec{},
		TTL:    time.Minute,
	})
	require.NoError(t, err)
	require.NoError(t, client.Set(ctx, "user:broken", "not-json", 0).Err())

	_, ok, err := cache.Get(ctx, "user:broken")
	require.Error(t, err)
	require.False(t, ok)
	require.EqualValues(t, 0, client.Exists(ctx, "user:broken").Val())
}
