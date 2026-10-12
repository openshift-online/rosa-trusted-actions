package main

import (
	"net/http"
	"testing"
)

func TestActionStore_PutAndGet(t *testing.T) {
	s := NewActionStore()

	entry := &ActionEntry{
		ClusterID:  "cluster-1",
		InstanceID: "instance-1",
		Name:       "test-action",
		Transport:  &http.Transport{},
		Token:      "bearer-token",
	}
	s.Put(entry)

	got, ok := s.Get("cluster-1", "instance-1")
	if !ok {
		t.Fatal("expected entry to be found")
	}
	if got.Name != "test-action" {
		t.Errorf("got Name %q, want %q", got.Name, "test-action")
	}
	if got.Token != "bearer-token" {
		t.Errorf("got Token %q, want %q", got.Token, "bearer-token")
	}
}

func TestActionStore_Get_NotFound(t *testing.T) {
	s := NewActionStore()

	_, ok := s.Get("cluster-1", "no-such-instance")
	if ok {
		t.Fatal("expected entry not to be found")
	}
}

func TestActionStore_CrossClusterIsolation(t *testing.T) {
	s := NewActionStore()

	s.Put(&ActionEntry{
		ClusterID:  "cluster-a",
		InstanceID: "instance-1",
		Name:       "action-a",
	})

	_, ok := s.Get("cluster-b", "instance-1")
	if ok {
		t.Fatal("instance from cluster-a should not be visible under cluster-b")
	}
}

func TestActionStore_Delete(t *testing.T) {
	s := NewActionStore()

	s.Put(&ActionEntry{
		ClusterID:  "cluster-1",
		InstanceID: "instance-1",
		Name:       "test-action",
	})

	if !s.Delete("cluster-1", "instance-1") {
		t.Fatal("expected Delete to return true for existing entry")
	}

	_, ok := s.Get("cluster-1", "instance-1")
	if ok {
		t.Fatal("expected entry to be gone after delete")
	}
}

func TestActionStore_Delete_NotFound(t *testing.T) {
	s := NewActionStore()

	if s.Delete("cluster-1", "no-such-instance") {
		t.Fatal("expected Delete to return false for missing entry")
	}
}
