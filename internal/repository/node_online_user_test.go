package repository

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/perfect-panel/server/internal/model/entity/node"
	"github.com/redis/go-redis/v9"
)

func newOnlineUserRepo(t *testing.T) (*nodeRepo, *miniredis.Miniredis, *redis.Client) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	repo, ok := newNodeRepo(nil, client).(*nodeRepo)
	if !ok {
		t.Fatal("newNodeRepo did not return *nodeRepo")
	}
	return repo, server, client
}

func requireOnlineUserCount(t *testing.T, repo NodeRepo, ctx context.Context, want int64) {
	t.Helper()
	got, err := repo.OnlineUserSubscribeGlobal(ctx)
	if err != nil {
		t.Fatalf("get global online user count: %v", err)
	}
	if got != want {
		t.Fatalf("global online user count = %d, want %d", got, want)
	}
}

func TestOnlineUserSnapshotReplacesMissingSubscriptions(t *testing.T) {
	repo, _, _ := newOnlineUserRepo(t)
	ctx := context.Background()

	if err := repo.UpdateOnlineUserSubscribe(ctx, 1, "shadowsocks", node.OnlineUserSubscribe{
		67: {"192.0.2.1", "192.0.2.2"},
		68: {"192.0.2.3"},
		69: {"192.0.2.4"},
	}); err != nil {
		t.Fatalf("write initial snapshot: %v", err)
	}
	requireOnlineUserCount(t, repo, ctx, 3)

	want := node.OnlineUserSubscribe{67: {"192.0.2.1"}}
	if err := repo.UpdateOnlineUserSubscribe(ctx, 1, "shadowsocks", want); err != nil {
		t.Fatalf("replace snapshot: %v", err)
	}
	requireOnlineUserCount(t, repo, ctx, 1)

	detail, err := repo.OnlineUserSubscribe(ctx, 1, "shadowsocks")
	if err != nil {
		t.Fatalf("read replaced snapshot: %v", err)
	}
	if !reflect.DeepEqual(detail, want) {
		t.Fatalf("snapshot = %#v, want %#v", detail, want)
	}
}

func TestOnlineUserGlobalDeduplicatesSources(t *testing.T) {
	repo, _, _ := newOnlineUserRepo(t)
	ctx := context.Background()

	snapshots := []struct {
		serverID int64
		protocol string
		users    node.OnlineUserSubscribe
	}{
		{1, "shadowsocks", node.OnlineUserSubscribe{67: {"192.0.2.1"}, 68: {"192.0.2.2"}}},
		{1, "vless", node.OnlineUserSubscribe{67: {"192.0.2.3"}, 69: {"192.0.2.4"}}},
		{2, "shadowsocks", node.OnlineUserSubscribe{67: {"192.0.2.5"}, 70: {"192.0.2.6"}}},
	}
	for _, snapshot := range snapshots {
		if err := repo.UpdateOnlineUserSubscribe(ctx, snapshot.serverID, snapshot.protocol, snapshot.users); err != nil {
			t.Fatalf("write %d/%s snapshot: %v", snapshot.serverID, snapshot.protocol, err)
		}
	}
	requireOnlineUserCount(t, repo, ctx, 4)

	if err := repo.UpdateOnlineUserSubscribe(ctx, 1, "shadowsocks", node.OnlineUserSubscribe{68: {"192.0.2.2"}}); err != nil {
		t.Fatalf("replace first source: %v", err)
	}
	requireOnlineUserCount(t, repo, ctx, 4)

	if err := repo.UpdateOnlineUserSubscribe(ctx, 1, "vless", node.OnlineUserSubscribe{}); err != nil {
		t.Fatalf("clear second source: %v", err)
	}
	requireOnlineUserCount(t, repo, ctx, 3)
}

func TestOnlineUserEmptySnapshotClearsSource(t *testing.T) {
	repo, server, client := newOnlineUserRepo(t)
	ctx := context.Background()
	const serverID int64 = 1
	const protocol = "shadowsocks"

	if err := repo.UpdateOnlineUserSubscribe(ctx, serverID, protocol, node.OnlineUserSubscribe{67: {"192.0.2.1"}}); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}
	if err := repo.UpdateOnlineUserSubscribe(ctx, serverID, protocol, node.OnlineUserSubscribe{}); err != nil {
		t.Fatalf("write empty snapshot: %v", err)
	}
	requireOnlineUserCount(t, repo, ctx, 0)

	detail, err := repo.OnlineUserSubscribe(ctx, serverID, protocol)
	if err != nil {
		t.Fatalf("read empty snapshot: %v", err)
	}
	if detail == nil || len(detail) != 0 {
		t.Fatalf("detail = %#v, want a non-nil empty map", detail)
	}

	sourceSetKey := fmt.Sprintf(node.OnlineUserSubscribeSetCacheKey, serverID, protocol)
	if server.Exists(sourceSetKey) {
		t.Fatalf("source set %q still exists", sourceSetKey)
	}
	if _, err := client.ZScore(ctx, node.OnlineUserSubscribeSourceIndexKey, sourceSetKey).Result(); !errors.Is(err, redis.Nil) {
		t.Fatalf("source index still contains %q, err = %v", sourceSetKey, err)
	}
}

func TestOnlineUserSourceExpiryAndLegacyCache(t *testing.T) {
	repo, server, client := newOnlineUserRepo(t)
	ctx := context.Background()

	if err := client.ZAdd(ctx, node.OnlineUserSubscribeCacheKeyWithGlobal, redis.Z{
		Score:  float64(time.Now().Add(time.Hour).Unix()),
		Member: 999,
	}).Err(); err != nil {
		t.Fatalf("seed legacy global cache: %v", err)
	}
	requireOnlineUserCount(t, repo, ctx, 0)

	if err := repo.UpdateOnlineUserSubscribe(ctx, 1, "shadowsocks", node.OnlineUserSubscribe{67: {"192.0.2.1"}}); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}
	requireOnlineUserCount(t, repo, ctx, 1)

	server.FastForward(node.Expiry + time.Second)
	requireOnlineUserCount(t, repo, ctx, 0)
	detail, err := repo.OnlineUserSubscribe(ctx, 1, "shadowsocks")
	if err != nil {
		t.Fatalf("read expired detail: %v", err)
	}
	if len(detail) != 0 {
		t.Fatalf("expired detail = %#v, want empty", detail)
	}
}

func TestDeleteOnlineUserSubscribeRemovesAllSourceData(t *testing.T) {
	repo, server, client := newOnlineUserRepo(t)
	ctx := context.Background()
	const serverID int64 = 1
	const protocol = "shadowsocks"

	if err := repo.UpdateOnlineUserSubscribe(ctx, serverID, protocol, node.OnlineUserSubscribe{67: {"192.0.2.1"}}); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}
	if err := repo.DeleteOnlineUserSubscribe(ctx, serverID, protocol); err != nil {
		t.Fatalf("delete snapshot: %v", err)
	}

	detailKey := fmt.Sprintf(node.OnlineUserCacheKeyWithSubscribe, serverID, protocol)
	sourceSetKey := fmt.Sprintf(node.OnlineUserSubscribeSetCacheKey, serverID, protocol)
	for _, key := range []string{detailKey, sourceSetKey} {
		if server.Exists(key) {
			t.Fatalf("cache key %q still exists", key)
		}
	}
	if _, err := client.ZScore(ctx, node.OnlineUserSubscribeSourceIndexKey, sourceSetKey).Result(); !errors.Is(err, redis.Nil) {
		t.Fatalf("source index still contains %q, err = %v", sourceSetKey, err)
	}
	requireOnlineUserCount(t, repo, ctx, 0)
}
