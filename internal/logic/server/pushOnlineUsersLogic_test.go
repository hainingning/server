package server

import (
	"context"
	"reflect"
	"testing"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/node"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
)

type pushOnlineUsersStore struct {
	repository.Store
	nodeRepo repository.NodeRepo
}

func (s *pushOnlineUsersStore) Node() repository.NodeRepo {
	return s.nodeRepo
}

type pushOnlineUsersNodeRepo struct {
	repository.NodeRepo
	findCalls       int
	updateCalls     int
	updatedServer   int64
	updatedProtocol string
	updatedUsers    node.OnlineUserSubscribe
}

func (r *pushOnlineUsersNodeRepo) FindOneServer(_ context.Context, id int64) (*node.Server, error) {
	r.findCalls++
	return &node.Server{Id: id}, nil
}

func (r *pushOnlineUsersNodeRepo) UpdateOnlineUserSubscribe(
	_ context.Context,
	serverID int64,
	protocol string,
	users node.OnlineUserSubscribe,
) error {
	r.updateCalls++
	r.updatedServer = serverID
	r.updatedProtocol = protocol
	r.updatedUsers = users
	return nil
}

func newPushOnlineUsersLogic(repo *pushOnlineUsersNodeRepo) *PushOnlineUsersLogic {
	store := &pushOnlineUsersStore{nodeRepo: repo}
	return NewPushOnlineUsersLogic(context.Background(), &svc.ServiceContext{Store: store})
}

func TestPushOnlineUsersAcceptsEmptySnapshot(t *testing.T) {
	repo := &pushOnlineUsersNodeRepo{}
	logic := newPushOnlineUsersLogic(repo)
	req := &dto.OnlineUsersRequest{
		ServerCommon: dto.ServerCommon{ServerId: 7, Protocol: "shadowsocks"},
		Users:        []dto.OnlineUser{},
	}

	if err := logic.PushOnlineUsers(req); err != nil {
		t.Fatalf("PushOnlineUsers() error = %v", err)
	}
	if repo.findCalls != 1 || repo.updateCalls != 1 {
		t.Fatalf("repository calls = find:%d update:%d, want 1 each", repo.findCalls, repo.updateCalls)
	}
	if repo.updatedServer != 7 || repo.updatedProtocol != "shadowsocks" {
		t.Fatalf("updated source = %d/%s, want 7/shadowsocks", repo.updatedServer, repo.updatedProtocol)
	}
	if repo.updatedUsers == nil || len(repo.updatedUsers) != 0 {
		t.Fatalf("updated users = %#v, want a non-nil empty map", repo.updatedUsers)
	}
}

func TestPushOnlineUsersRejectsInvalidRequestsBeforeRepositoryAccess(t *testing.T) {
	tests := []struct {
		name string
		req  *dto.OnlineUsersRequest
	}{
		{
			name: "invalid server id",
			req:  &dto.OnlineUsersRequest{Users: []dto.OnlineUser{}},
		},
		{
			name: "missing users",
			req:  &dto.OnlineUsersRequest{ServerCommon: dto.ServerCommon{ServerId: 1}},
		},
		{
			name: "invalid subscription id",
			req: &dto.OnlineUsersRequest{
				ServerCommon: dto.ServerCommon{ServerId: 1},
				Users:        []dto.OnlineUser{{SID: 0, IP: "192.0.2.1"}},
			},
		},
		{
			name: "missing ip",
			req: &dto.OnlineUsersRequest{
				ServerCommon: dto.ServerCommon{ServerId: 1},
				Users:        []dto.OnlineUser{{SID: 67}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &pushOnlineUsersNodeRepo{}
			logic := newPushOnlineUsersLogic(repo)
			if err := logic.PushOnlineUsers(tt.req); err == nil {
				t.Fatal("PushOnlineUsers() error = nil, want validation error")
			}
			if repo.findCalls != 0 || repo.updateCalls != 0 {
				t.Fatalf("repository calls = find:%d update:%d, want 0", repo.findCalls, repo.updateCalls)
			}
		})
	}
}

func TestPushOnlineUsersGroupsConnectionsBySubscription(t *testing.T) {
	repo := &pushOnlineUsersNodeRepo{}
	logic := newPushOnlineUsersLogic(repo)
	req := &dto.OnlineUsersRequest{
		ServerCommon: dto.ServerCommon{ServerId: 7, Protocol: "shadowsocks"},
		Users: []dto.OnlineUser{
			{SID: 67, IP: "192.0.2.1"},
			{SID: 67, IP: "192.0.2.2"},
			{SID: 68, IP: "192.0.2.3"},
		},
	}

	if err := logic.PushOnlineUsers(req); err != nil {
		t.Fatalf("PushOnlineUsers() error = %v", err)
	}
	want := node.OnlineUserSubscribe{
		67: {"192.0.2.1", "192.0.2.2"},
		68: {"192.0.2.3"},
	}
	if !reflect.DeepEqual(repo.updatedUsers, want) {
		t.Fatalf("updated users = %#v, want %#v", repo.updatedUsers, want)
	}
}
