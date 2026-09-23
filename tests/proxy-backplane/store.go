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

type ActionStore struct {
	mu      sync.RWMutex
	entries map[string]*ActionEntry
}

func NewActionStore() *ActionStore {
	return &ActionStore{
		entries: make(map[string]*ActionEntry),
	}
}

func storeKey(clusterID, instanceID string) string {
	return clusterID + ":" + instanceID
}

func (s *ActionStore) Put(entry *ActionEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[storeKey(entry.ClusterID, entry.InstanceID)] = entry
}

func (s *ActionStore) Get(clusterID, instanceID string) (*ActionEntry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.entries[storeKey(clusterID, instanceID)]
	return e, ok
}

func (s *ActionStore) Delete(clusterID, instanceID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := storeKey(clusterID, instanceID)
	if _, ok := s.entries[key]; !ok {
		return false
	}
	delete(s.entries, key)
	return true
}
