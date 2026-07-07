package oauth

import (
	"errors"
	"sync"
	"time"
)

var ErrCodeAlreadyUsed = errors.New("authorization code ID is unknown, expired, or already used")

type CodeStore struct {
	mu      sync.Mutex
	now     func() time.Time
	max     int
	entries map[string]time.Time
}

func NewCodeStore(maxEntries int, now func() time.Time) *CodeStore {
	if maxEntries <= 0 {
		maxEntries = 1024
	}
	if now == nil {
		now = time.Now
	}
	return &CodeStore{
		now:     now,
		max:     maxEntries,
		entries: make(map[string]time.Time),
	}
}

func (s *CodeStore) Store(id string, expiresAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.purgeExpiredLocked()
	if id == "" {
		return errors.New("authorization code ID is required")
	}
	if !expiresAt.After(s.now()) {
		return errors.New("authorization code expiry must be in the future")
	}
	if len(s.entries) >= s.max {
		return errors.New("authorization code store is full")
	}
	if _, exists := s.entries[id]; exists {
		return errors.New("authorization code ID already exists")
	}
	s.entries[id] = expiresAt
	return nil
}

func (s *CodeStore) Consume(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.purgeExpiredLocked()
	expiresAt, exists := s.entries[id]
	if !exists || !expiresAt.After(s.now()) {
		return ErrCodeAlreadyUsed
	}
	delete(s.entries, id)
	return nil
}

func (s *CodeStore) purgeExpiredLocked() {
	now := s.now()
	for id, expiresAt := range s.entries {
		if !expiresAt.After(now) {
			delete(s.entries, id)
		}
	}
}
