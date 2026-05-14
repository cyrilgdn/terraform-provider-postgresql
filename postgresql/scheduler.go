package postgresql

import (
	"context"
	"sync"
)

// dbScheduler caps how many distinct databases may be operated on
// concurrently within a single provider process. It is reentrant by database
// name: additional goroutines targeting an already-active database share its
// slot via refcount, so resources on the active database keep parallelizing
// up to terraform's per-resource parallelism. Resources on inactive databases
// block until a slot frees up.
//
// Acquire is called at the resource CRUD boundary (PGResourceFunc /
// PGResourceExistsFunc) keyed by the actual target database — resolved via
// targetDatabaseFromAttr from a resource-specific attribute (default
// "database"; postgresql_database overrides to "name") with fallback to
// client.databaseName for resources that operate on the maintenance DB.
// Nested startTransaction switches to a different target database
// deliberately do NOT take a second slot: with cap=1 only one goroutine is
// in flight, so per-goroutine fan-out across databases is safe and avoids
// self-deadlock.
type dbScheduler struct {
	mu     sync.Mutex
	cond   *sync.Cond
	cap    int
	active map[string]int
}

func newDBScheduler(cap int) *dbScheduler {
	s := &dbScheduler{cap: cap, active: make(map[string]int)}
	s.cond = sync.NewCond(&s.mu)
	return s
}

// Acquire blocks until the caller may operate on db. The returned release
// function must be called exactly once when the caller is done.
//
// When ctx is cancelled before a slot is granted, Acquire returns ctx.Err().
// A watcher goroutine broadcasts on the cond so a cancelled waiter doesn't
// stay parked.
func (s *dbScheduler) Acquire(ctx context.Context, db string) (func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// If ctx can be cancelled, run a watcher that wakes all waiters so the
	// cancelled goroutine notices ctx.Done on the next loop iteration. Stops
	// when the caller finishes Acquire (success or failure).
	done := make(chan struct{})
	if ctx.Done() != nil {
		go func() {
			select {
			case <-ctx.Done():
				s.mu.Lock()
				s.cond.Broadcast()
				s.mu.Unlock()
			case <-done:
			}
		}()
	}
	defer close(done)

	for {
		if _, ok := s.active[db]; ok {
			s.active[db]++
			return s.releaseFunc(db), nil
		}
		if len(s.active) < s.cap {
			s.active[db] = 1
			return s.releaseFunc(db), nil
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		s.cond.Wait()
	}
}

func (s *dbScheduler) releaseFunc(db string) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			s.active[db]--
			if s.active[db] <= 0 {
				delete(s.active, db)
				s.cond.Broadcast()
			}
		})
	}
}
