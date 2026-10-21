package memcache_test

import (
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/huncoding/payment-processor-course/pkg/memcache"
)

func TestSetGet(t *testing.T) {
	c := memcache.New[string](time.Minute, time.Minute)
	defer c.Close()

	c.Set("key", "value")
	v, ok := c.Get("key")
	if !ok {
		t.Fatalf("expected key to exist\n")
	}
	if v != "value" {
		t.Errorf("expected value, got %s\n", v)
	}
}

func TestExpiration(t *testing.T) {
	c := memcache.New[int](10*time.Millisecond, time.Hour)
	defer c.Close()

	c.Set("key", 42)
	time.Sleep(30*time.Millisecond)

	_, ok := c.Get("key")
	if ok {
		t.Errorf("expected key to be expired\n")
	}
}

func TestIncrementIsAtomic(t *testing.T) {
	c := memcache.New[int](time.Minute, time.Minute)
	defer c.Close()

	add := func(current int, exists bool) int {
		if !exists {
			return 1
		}
		return current + 1
	}

	const goroutines = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func ()  {
			defer wg.Done()
			c.Increment("card:123", add)
		}()
	}
	wg.Wait()

	v, ok := c.Get("card:123")
	if !ok {
		t.Fatalf("expected key to exist")
	}
	if v != goroutines {
		t.Errorf("expected %d, got %d\n", goroutines, v)
	}
}

func TestConcurrentAccessAcrossShards(t *testing.T) {
	c := memcache.New[int](time.Minute, time.Minute)
	defer c.Close()

	const goroutines = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func (val int)  {
			defer wg.Done()
			key := fmt.Sprintf("key-%d", val%50)
			c.Set(key, val)
			_, ok := c.Get(key)
			if !ok {
				t.Errorf("expected key %s to exist", key)
			}
		}(i)
	}
	wg.Wait()
}

func TestJanitorEvictsExpired(t *testing.T) {
	c := memcache.New[int](10*time.Millisecond, 1*time.Millisecond)
	defer c.Close()

	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("key-%d", i)
		if err := c.Set(key, i); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	deadline := time.Now().Add(500*time.Millisecond)
	for time.Now().Before(deadline) {
		if c.Len() == 0 {
			return
		}
		runtime.Gosched()
	}

	t.Errorf("janitor did not evict expired items within deadline, got %d", c.Len())
}