package notify

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// recordingInserter captures every event it is asked to insert.
type recordingInserter struct {
	mu     sync.Mutex
	events []Event
}

func (r *recordingInserter) Insert(_ context.Context, ev Event) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, ev)
	return int64(len(r.events)), nil
}

func (r *recordingInserter) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.events)
}

// erroringInserter always fails.
type erroringInserter struct{ err error }

func (e erroringInserter) Insert(context.Context, Event) (int64, error) { return 0, e.err }

// blockingInserter blocks every insert until release is closed, signalling the
// first entry on started so a test can synchronize on the worker being busy.
type blockingInserter struct {
	started chan struct{}
	release chan struct{}
	count   atomic.Int64
}

func (b *blockingInserter) Insert(context.Context, Event) (int64, error) {
	select {
	case b.started <- struct{}{}:
	default:
	}
	<-b.release
	return b.count.Add(1), nil
}

func newTestAsync(store inserter, buf int) (*asyncEmitter, func(context.Context) error) {
	e := &asyncEmitter{store: store, log: quietLogger(), ch: make(chan Event, buf)}
	e.wg.Add(1)
	go e.run()
	return e, e.drain
}

func TestSyncEmitter(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		rec := &recordingInserter{}
		e := &syncEmitter{store: rec, log: quietLogger()}
		if err := e.Emit(context.Background(), validEvent()); err != nil {
			t.Fatalf("Emit() err = %v", err)
		}
		if rec.count() != 1 {
			t.Fatalf("recorded %d events, want 1", rec.count())
		}
	})

	t.Run("propagates store error", func(t *testing.T) {
		t.Parallel()
		sentinel := errors.New("boom")
		e := &syncEmitter{store: erroringInserter{err: sentinel}, log: quietLogger()}
		if err := e.Emit(context.Background(), validEvent()); !errors.Is(err, sentinel) {
			t.Fatalf("Emit() err = %v, want %v", err, sentinel)
		}
	})
}

func TestAsyncEmitterFlushesOnDrain(t *testing.T) {
	t.Parallel()
	rec := &recordingInserter{}
	e, drain := newTestAsync(rec, 8)

	for range 3 {
		if err := e.Emit(context.Background(), validEvent()); err != nil {
			t.Fatalf("Emit() err = %v", err)
		}
	}
	if err := drain(context.Background()); err != nil {
		t.Fatalf("drain() err = %v", err)
	}
	if rec.count() != 3 {
		t.Fatalf("recorded %d events, want 3", rec.count())
	}
}

func TestAsyncEmitterValidatesBeforeEnqueue(t *testing.T) {
	t.Parallel()
	rec := &recordingInserter{}
	e, drain := newTestAsync(rec, 8)
	defer func() { _ = drain(context.Background()) }()

	err := e.Emit(context.Background(), Event{Source: "osg", Title: "no type"})
	if !errors.Is(err, ErrMissingType) {
		t.Fatalf("Emit() err = %v, want ErrMissingType", err)
	}
}

func TestAsyncEmitterDropsWhenFull(t *testing.T) {
	t.Parallel()
	b := &blockingInserter{started: make(chan struct{}, 1), release: make(chan struct{})}
	e, drain := newTestAsync(b, 1)

	// First event is pulled by the worker, which then blocks inside Insert.
	if err := e.Emit(context.Background(), validEvent()); err != nil {
		t.Fatalf("Emit() err = %v", err)
	}
	<-b.started // worker is now blocked; the channel is empty

	// Fills the single buffer slot.
	if err := e.Emit(context.Background(), validEvent()); err != nil {
		t.Fatalf("Emit() err = %v", err)
	}
	// Buffer full → dropped, but Emit still returns nil.
	if err := e.Emit(context.Background(), validEvent()); err != nil {
		t.Fatalf("Emit() err = %v", err)
	}
	if got := e.Dropped(); got != 1 {
		t.Fatalf("Dropped() = %d, want 1", got)
	}

	close(b.release)
	if err := drain(context.Background()); err != nil {
		t.Fatalf("drain() err = %v", err)
	}
	if got := b.count.Load(); got != 2 {
		t.Fatalf("inserted %d events, want 2", got)
	}
}

func TestAsyncEmitterClosedAfterDrain(t *testing.T) {
	t.Parallel()
	rec := &recordingInserter{}
	e, drain := newTestAsync(rec, 4)
	if err := drain(context.Background()); err != nil {
		t.Fatalf("drain() err = %v", err)
	}
	if err := e.Emit(context.Background(), validEvent()); !errors.Is(err, ErrEmitterClosed) {
		t.Fatalf("Emit() after drain err = %v, want ErrEmitterClosed", err)
	}
	// Second drain is a no-op.
	if err := drain(context.Background()); err != nil {
		t.Fatalf("second drain() err = %v", err)
	}
}

func TestAsyncEmitterDrainRespectsContext(t *testing.T) {
	t.Parallel()
	b := &blockingInserter{started: make(chan struct{}, 1), release: make(chan struct{})}
	e, drain := newTestAsync(b, 4)

	if err := e.Emit(context.Background(), validEvent()); err != nil {
		t.Fatalf("Emit() err = %v", err)
	}
	<-b.started // worker stuck in Insert and will not finish

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := drain(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("drain() err = %v, want DeadlineExceeded", err)
	}

	// Let the worker exit so the goroutine leak checker stays happy. drain has
	// already closed the channel, so once the blocked insert returns the worker
	// drains the (empty) channel and exits.
	close(b.release)
	e.wg.Wait()
}
