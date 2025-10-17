package store

import (
	"context"
	"errors"
	"sync"
	"time"

	"encoding/json"

	"github.com/redis/go-redis/v9"

	"ytmp3api/internal/models"
)

type SessionStore interface {
	CreateSession(ctx context.Context, s *models.ConversionSession) error
	UpdateSession(ctx context.Context, s *models.ConversionSession) error
	GetSession(ctx context.Context, id string) (*models.ConversionSession, error)
	DeleteSession(ctx context.Context, id string) error
	FindByURL(ctx context.Context, url string) (string, bool, error)
	SetURLMap(ctx context.Context, url, id string) error
	// Optional helpers for dedup caches (no-op for memory store unless implemented)
	SetVariant(ctx context.Context, variantHash, outputPath string) error
	GetVariant(ctx context.Context, variantHash string) (string, bool, error)
	SetAsset(ctx context.Context, assetHash, sourcePath, state string) error
	GetAsset(ctx context.Context, assetHash string) (sourcePath string, state string, ok bool, err error)
}

var ErrNotFound = errors.New("not found")

// MemoryStore implements in-memory sessions with URL deduplication.
type MemoryStore struct {
	mu           sync.RWMutex
	sessions     map[string]*models.ConversionSession
	urlToID      map[string]string
	variantToOut map[string]string
	assetMap     map[string]struct {
		SourcePath string
		State      string
	}
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		sessions:     make(map[string]*models.ConversionSession),
		urlToID:      make(map[string]string),
		variantToOut: make(map[string]string),
		assetMap: make(map[string]struct {
			SourcePath string
			State      string
		}),
	}
}

func (m *MemoryStore) CreateSession(ctx context.Context, s *models.ConversionSession) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[s.ID]; ok {
		return errors.New("session exists")
	}
	s.CreatedAt = time.Now().UTC()
	s.UpdatedAt = s.CreatedAt
	m.sessions[s.ID] = s
	return nil
}

func (m *MemoryStore) UpdateSession(ctx context.Context, s *models.ConversionSession) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[s.ID]; !ok {
		return ErrNotFound
	}
	s.UpdatedAt = time.Now().UTC()
	m.sessions[s.ID] = s
	return nil
}

func (m *MemoryStore) GetSession(ctx context.Context, id string) (*models.ConversionSession, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[id]
	if !ok {
		return nil, ErrNotFound
	}
	copy := *s
	return &copy, nil
}

func (m *MemoryStore) DeleteSession(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, id)
	for u, sid := range m.urlToID {
		if sid == id {
			delete(m.urlToID, u)
		}
	}
	return nil
}

func (m *MemoryStore) FindByURL(ctx context.Context, url string) (string, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	sid, ok := m.urlToID[url]
	return sid, ok, nil
}

func (m *MemoryStore) SetURLMap(ctx context.Context, url, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.urlToID[url] = id
	return nil
}

func (m *MemoryStore) SetVariant(ctx context.Context, variantHash, outputPath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.variantToOut[variantHash] = outputPath
	return nil
}

func (m *MemoryStore) GetVariant(ctx context.Context, variantHash string) (string, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.variantToOut[variantHash]
	return p, ok, nil
}

func (m *MemoryStore) SetAsset(ctx context.Context, assetHash, sourcePath, state string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.assetMap[assetHash] = struct {
		SourcePath string
		State      string
	}{SourcePath: sourcePath, State: state}
	return nil
}

func (m *MemoryStore) GetAsset(ctx context.Context, assetHash string) (string, string, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	a, ok := m.assetMap[assetHash]
	if !ok {
		return "", "", false, nil
	}
	return a.SourcePath, a.State, true, nil
}

// RedisStore implements SessionStore on Redis.
type RedisStore struct {
	rdb *redis.Client
}

func NewRedisStore(rdb *redis.Client) *RedisStore { return &RedisStore{rdb: rdb} }

func (r *RedisStore) sessionKey(id string) string { return "session:" + id }
func (r *RedisStore) urlKey(url string) string    { return "url:" + url }

func (r *RedisStore) CreateSession(ctx context.Context, s *models.ConversionSession) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return r.rdb.Set(ctx, r.sessionKey(s.ID), b, 0).Err()
}
func (r *RedisStore) UpdateSession(ctx context.Context, s *models.ConversionSession) error {
	s.UpdatedAt = time.Now().UTC()
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return r.rdb.Set(ctx, r.sessionKey(s.ID), b, 0).Err()
}
func (r *RedisStore) GetSession(ctx context.Context, id string) (*models.ConversionSession, error) {
	res, err := r.rdb.Get(ctx, r.sessionKey(id)).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var s models.ConversionSession
	if err := json.Unmarshal(res, &s); err != nil {
		return nil, err
	}
	return &s, nil
}
func (r *RedisStore) DeleteSession(ctx context.Context, id string) error {
	return r.rdb.Del(ctx, r.sessionKey(id)).Err()
}
func (r *RedisStore) FindByURL(ctx context.Context, url string) (string, bool, error) {
	id, err := r.rdb.Get(ctx, r.urlKey(url)).Result()
	if err != nil {
		if err == redis.Nil {
			return "", false, nil
		}
		return "", false, err
	}
	return id, true, nil
}
func (r *RedisStore) SetURLMap(ctx context.Context, url, id string) error {
	return r.rdb.Set(ctx, r.urlKey(url), id, 24*time.Hour).Err()
}

func (r *RedisStore) SetVariant(ctx context.Context, variantHash, outputPath string) error {
	key := "variant:" + variantHash
	return r.rdb.Set(ctx, key, outputPath, 24*time.Hour).Err()
}

func (r *RedisStore) GetVariant(ctx context.Context, variantHash string) (string, bool, error) {
	key := "variant:" + variantHash
	v, err := r.rdb.Get(ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			return "", false, nil
		}
		return "", false, err
	}
	return v, true, nil
}

func (r *RedisStore) SetAsset(ctx context.Context, assetHash, sourcePath, state string) error {
	key := "asset:" + assetHash
	payload := map[string]string{"source_path": sourcePath, "state": state}
	b, _ := json.Marshal(payload)
	return r.rdb.Set(ctx, key, b, 24*time.Hour).Err()
}

func (r *RedisStore) GetAsset(ctx context.Context, assetHash string) (string, string, bool, error) {
	key := "asset:" + assetHash
	b, err := r.rdb.Get(ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return "", "", false, nil
		}
		return "", "", false, err
	}
	var p struct {
		SourcePath string `json:"source_path"`
		State      string `json:"state"`
	}
	if err := json.Unmarshal(b, &p); err != nil {
		return "", "", false, err
	}
	return p.SourcePath, p.State, true, nil
}
