package main

import (
	"net/http"
	"sync"
)

type ActionEntry struct {
	ClusterID          string
	InstanceID         string
	Name               string
	CustomerDataAccess string
	Host               string
	Transport          *http.Transport
	Token              string
}

type storeKey struct {
	clusterID  string
	instanceID string
}

type ActionStore struct {
	mu      sync.RWMutex
	entries map[storeKey]*ActionEntry
}

func NewActionStore() *ActionStore {
	return &ActionStore{
		entries: make(map[storeKey]*ActionEntry),
	}
}

func (s *ActionStore) Put(entry *ActionEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[storeKey{entry.ClusterID, entry.InstanceID}] = entry
}

func (s *ActionStore) Get(clusterID, instanceID string) (*ActionEntry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.entries[storeKey{clusterID, instanceID}]
	return e, ok
}

func (s *ActionStore) Delete(clusterID, instanceID string) bool {
	s.mu.Lock()
	key := storeKey{clusterID, instanceID}
	entry, ok := s.entries[key]
	if !ok {
		s.mu.Unlock()
		return false
	}
	delete(s.entries, key)
	s.mu.Unlock()
	if entry.Transport != nil {
		entry.Transport.CloseIdleConnections()
	}
	return true
}
