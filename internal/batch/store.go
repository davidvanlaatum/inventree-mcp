package batch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/davidvanlaatum/inventree-mcp/internal/platform"
)

const (
	defaultLifetime               = 5 * time.Minute
	defaultMaxEntries             = 4096
	defaultMaxEntriesPerPrincipal = 64
)

// ErrCapacity is returned by Store.Issue when the store already holds the
// maximum number of outstanding confirmation tokens, either globally or for
// the issuing principal.
var ErrCapacity = errors.New("too many outstanding batch confirmation plans")

// Digestible is implemented by resource-specific batch plan types so Store
// can bind a confirmation token to the plan's complete current state without
// reflecting over its shape. A typical implementation is one line:
//
//	func (p MyBatchPlan) Digest() (string, error) { return batch.JSONDigest(p) }
//
// Because Digest normally hashes the plan's ordered item slice along with
// every item's captured "before" snapshot, a changed, reordered, or
// truncated batch naturally produces a different digest and is rejected by
// Consume without any batch-specific comparison logic.
type Digestible interface {
	Digest() (string, error)
}

// JSONDigest hashes v's canonical JSON encoding with SHA-256 and returns the
// hex-encoded digest. It is the standard building block for a plan type's
// Digest method.
func JSONDigest(v any) (string, error) {
	encoded, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

// Options configures a Store. IDGenerator, Principal, and SupersedeKey are
// required; the remaining fields fall back to package defaults matching the
// existing single-item confirmation stores (five-minute lifetime, 4096
// global entries, 64 entries per principal).
type Options[P Digestible] struct {
	Clock       platform.Clock
	IDGenerator platform.IDGenerator
	Principal   func(context.Context) string
	// SupersedeKey identifies the batch operation a plan belongs to, scoped
	// per principal: issuing a new token for the same principal+key
	// invalidates any earlier unexpired token for that key. Combine fields
	// with a delimiter unlikely to appear in them (for example
	// fmt.Sprintf("%s\x00%d", action, id)) rather than plain concatenation,
	// or distinct plans can collide onto the same key.
	SupersedeKey           func(P) string
	Lifetime               time.Duration
	MaxEntries             int
	MaxEntriesPerPrincipal int
}

type entry struct {
	digest       string
	principal    string
	supersedeKey string
	expiresAt    time.Time
}

// Store issues and consumes single-use, principal-bound confirmation tokens
// for batch plans of type P. It generalizes the confirmation-token/digest
// pattern used by this repository's existing single-item mutation tools
// (see internal/tools' *PlanStore types) to any batch plan shape without
// requiring a new hand-copied store per resource family.
type Store[P Digestible] struct {
	mu      sync.Mutex
	entries map[string]entry
	opts    Options[P]
}

// NewStore builds a Store from opts, applying documented defaults for any
// zero-valued optional field. It returns an error if a required field is
// missing.
func NewStore[P Digestible](opts Options[P]) (*Store[P], error) {
	if opts.IDGenerator == nil {
		return nil, errors.New("batch: Options.IDGenerator is required")
	}
	if opts.Principal == nil {
		return nil, errors.New("batch: Options.Principal is required")
	}
	if opts.SupersedeKey == nil {
		return nil, errors.New("batch: Options.SupersedeKey is required")
	}
	if opts.Clock == nil {
		opts.Clock = platform.RealClock{}
	}
	if opts.Lifetime <= 0 {
		opts.Lifetime = defaultLifetime
	}
	if opts.MaxEntries <= 0 {
		opts.MaxEntries = defaultMaxEntries
	}
	if opts.MaxEntriesPerPrincipal <= 0 {
		opts.MaxEntriesPerPrincipal = defaultMaxEntriesPerPrincipal
	}
	return &Store[P]{entries: map[string]entry{}, opts: opts}, nil
}

// Issue binds a fresh single-use token to plan's current digest, principal,
// and expiry. Issuing a new token for the same principal and SupersedeKey
// deletes any earlier still-unexpired token for that key, so a fresh dry run
// invalidates a stale one before it expires.
func (s *Store[P]) Issue(ctx context.Context, plan P) (string, error) {
	digest, err := plan.Digest()
	if err != nil {
		return "", err
	}
	token, err := s.opts.IDGenerator.NewID(ctx)
	if err != nil {
		return "", err
	}
	now := s.opts.Clock.Now()
	principal := s.opts.Principal(ctx)
	supersedeKey := s.opts.SupersedeKey(plan)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleteExpiredLocked(now)

	principalEntries := 0
	for existingToken, existing := range s.entries {
		if existing.principal != principal {
			continue
		}
		if existing.supersedeKey == supersedeKey {
			delete(s.entries, existingToken)
			continue
		}
		principalEntries++
	}
	if len(s.entries) >= s.opts.MaxEntries || principalEntries >= s.opts.MaxEntriesPerPrincipal {
		return "", ErrCapacity
	}

	s.entries[token] = entry{
		digest:       digest,
		principal:    principal,
		supersedeKey: supersedeKey,
		expiresAt:    now.Add(s.opts.Lifetime),
	}
	return token, nil
}

// Consume validates token against plan's current digest, the calling
// principal, and expiry, deleting the token if it is valid. It returns
// false without mutating the store for a missing, expired, mismatched, or
// wrong-principal token, so a failed confirmation never burns a still-valid
// token belonging to someone else's concurrent attempt.
func (s *Store[P]) Consume(ctx context.Context, token string, plan P) bool {
	digest, err := plan.Digest()
	if err != nil {
		return false
	}
	now := s.opts.Clock.Now()
	principal := s.opts.Principal(ctx)
	supersedeKey := s.opts.SupersedeKey(plan)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleteExpiredLocked(now)

	existing, ok := s.entries[token]
	if !ok {
		return false
	}
	valid := existing.digest == digest &&
		existing.principal == principal &&
		existing.supersedeKey == supersedeKey &&
		now.Before(existing.expiresAt)
	if valid {
		delete(s.entries, token)
	}
	return valid
}

func (s *Store[P]) deleteExpiredLocked(now time.Time) {
	for token, existing := range s.entries {
		if !now.Before(existing.expiresAt) {
			delete(s.entries, token)
		}
	}
}
