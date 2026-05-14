package postgresql

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// Two goroutines on different databases with cap=1: the second must wait
// until the first releases.
func TestScheduler_CapOneSerializesDifferentDBs(t *testing.T) {
	s := newDBScheduler(1)
	ctx := context.Background()

	relA, err := s.Acquire(ctx, "a")
	assert.NoError(t, err)

	bAcquired := make(chan struct{})
	go func() {
		relB, err := s.Acquire(ctx, "b")
		assert.NoError(t, err)
		close(bAcquired)
		relB()
	}()

	select {
	case <-bAcquired:
		t.Fatal("b acquired while a was holding the only slot")
	case <-time.After(50 * time.Millisecond):
	}

	relA()

	select {
	case <-bAcquired:
	case <-time.After(time.Second):
		t.Fatal("b was not granted after a released")
	}
}

// With cap=1, goroutines targeting the same db proceed in parallel — they
// share a slot via refcount.
func TestScheduler_RefcountSameDB(t *testing.T) {
	s := newDBScheduler(1)
	ctx := context.Background()

	rel1, err := s.Acquire(ctx, "a")
	assert.NoError(t, err)

	acquired := make(chan struct{})
	var rel2 func()
	go func() {
		rel2, err = s.Acquire(ctx, "a")
		assert.NoError(t, err)
		close(acquired)
	}()

	select {
	case <-acquired:
	case <-time.After(time.Second):
		t.Fatal("second goroutine on same db blocked despite refcount")
	}

	rel1()
	rel2()
	assert.Equal(t, 0, s.refcountForTest("a"))
}

// After release, exactly one waiter is granted. Repeated until all waiters
// drain.
func TestScheduler_FairnessAfterRelease(t *testing.T) {
	s := newDBScheduler(1)
	ctx := context.Background()

	rel, _ := s.Acquire(ctx, "a")

	const N = 5
	var wg sync.WaitGroup
	var counter atomic.Int32
	for i := 0; i < N; i++ {
		wg.Add(1)
		dbName := string(rune('b' + i))
		go func() {
			defer wg.Done()
			r, err := s.Acquire(ctx, dbName)
			assert.NoError(t, err)
			counter.Add(1)
			time.Sleep(10 * time.Millisecond)
			r()
		}()
	}

	time.Sleep(50 * time.Millisecond)
	assert.EqualValues(t, 0, counter.Load(), "no waiter should proceed while slot is held")

	rel()
	wg.Wait()
	assert.EqualValues(t, N, counter.Load())
}

// Acquire returns ctx.Err() when the context is cancelled while waiting.
func TestScheduler_ContextCancelWhileWaiting(t *testing.T) {
	s := newDBScheduler(1)

	rel, _ := s.Acquire(context.Background(), "a")
	defer rel()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := s.Acquire(ctx, "b")
	elapsed := time.Since(start)

	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, elapsed, 500*time.Millisecond)
}

// Acquire returns ctx.Err() immediately when context is already cancelled.
func TestScheduler_ContextAlreadyCancelled(t *testing.T) {
	s := newDBScheduler(1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := s.Acquire(ctx, "a")
	assert.ErrorIs(t, err, context.Canceled)
}

// Invariant under load: len(active) never exceeds cap, refcounts settle to 0.
func TestScheduler_HighConcurrencyInvariant(t *testing.T) {
	const (
		cap          = 2
		databases    = 5
		goroutines   = 100
		opsPerWorker = 5
	)
	s := newDBScheduler(cap)
	ctx := context.Background()

	var wg sync.WaitGroup
	var maxSeen atomic.Int32
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < opsPerWorker; i++ {
				db := string(rune('a' + (g+i)%databases))
				rel, err := s.Acquire(ctx, db)
				if err != nil {
					t.Errorf("acquire failed: %v", err)
					return
				}
				s.mu.Lock()
				cur := int32(len(s.active))
				s.mu.Unlock()
				for {
					prev := maxSeen.Load()
					if cur <= prev || maxSeen.CompareAndSwap(prev, cur) {
						break
					}
				}
				rel()
			}
		}(g)
	}
	wg.Wait()

	assert.LessOrEqual(t, int(maxSeen.Load()), cap, "len(active) must never exceed cap")

	s.mu.Lock()
	leftover := len(s.active)
	s.mu.Unlock()
	assert.Equal(t, 0, leftover, "all slots must be released")
}

// Calling release more than once must not over-decrement the refcount.
func TestScheduler_ReleaseIdempotent(t *testing.T) {
	s := newDBScheduler(1)
	rel, _ := s.Acquire(context.Background(), "a")
	rel()
	rel()
	assert.Equal(t, 0, s.refcountForTest("a"))
}

// Test helper — read refcount under lock.
func (s *dbScheduler) refcountForTest(db string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active[db]
}
