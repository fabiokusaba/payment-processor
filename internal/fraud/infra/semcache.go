package infra

import (
	"sync"
	"time"

	"github.com/huncoding/payment-processor-course/internal/fraud/domain"
)

// SemanticCache remembers recent scored attempts by embedding. When a new ambiguous attempt
// is similar enough to a recent one, the cached verdict breaks the tie without any extra model call
type SemanticCache struct {
	mu        sync.RWMutex
	entries   []semEntry
	maxSize   int
	ttl       time.Duration
	threshold float64
	now       func() time.Time
}

type semEntry struct {
	vector    []float64
	verdict   domain.Verdict
	expiresAt time.Time
}

// NewSemanticCache builds the cache, threshold is the minimum cosine similarity for a hit
func NewSemanticCache(maxSize int, ttl time.Duration, threshold float64) *SemanticCache {
	return &SemanticCache{
		maxSize:   maxSize,
		ttl:       ttl,
		threshold: threshold,
		now:       time.Now,
	}
}

// Lookup returns the verdict of the most similar cached attempt when similarity clears the threshold
func (c *SemanticCache) Lookup(vector []float64) (domain.Verdict, float64, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	now := c.now()
	bestSim := -1.0
	var bestVerdict domain.Verdict

	for i := range c.entries {
		if now.After(c.entries[i].expiresAt) {
			continue
		}

		sim, err := Cosine(vector, c.entries[i].vector)
		if err != nil {
			continue
		}

		if sim > bestSim {
			bestSim = sim
			bestVerdict = c.entries[i].verdict
		}
	}

	if bestSim >= c.threshold {
		return bestVerdict, bestSim, true
	}

	return "", bestSim, false
}

// Store adds a new attempt to the cache
func (c *SemanticCache) Store(vector []float64, verdict domain.Verdict) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry := semEntry{
		vector:    vector,
		verdict:   verdict,
		expiresAt: c.now().Add(c.ttl),
	}

	if len(c.entries) >= c.maxSize {
		c.entries = c.entries[1:]
	}

	c.entries = append(c.entries, entry)
}

// Len returns the number of cached entries
func (c *SemanticCache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}
