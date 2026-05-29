package notify

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// Emitter is the producer surface a host wires into its handlers. Call Emit at
// each event point. The host should treat Emit as best-effort and must not let
// a notification failure break the user flow.
type Emitter interface {
	Emit(ctx context.Context, ev Event) error
}

// ErrEmitterClosed is returned by an async Emitter's Emit after its Drain has
// been called.
var ErrEmitterClosed = errors.New("notify: emitter closed")

// inserter is the subset of [Store] the emitters depend on, so tests can
// substitute a fake without a database.
type inserter interface {
	Insert(ctx context.Context, ev Event) (int64, error)
}

// writeTimeout bounds a single background insert so one slow write cannot wedge
// the async worker indefinitely.
const writeTimeout = 10 * time.Second

// syncEmitter writes inline and returns the store error. Use it when the host
// genuinely wants the write in its own request path (or transaction).
type syncEmitter struct {
	store inserter
	log   *slog.Logger
}

// NewSyncEmitter returns an Emitter that writes synchronously and surfaces the
// store error to the caller. Prefer [NewAsyncEmitter] for request-path use.
func NewSyncEmitter(store *Store, log *slog.Logger) Emitter {
	return &syncEmitter{store: store, log: orDefault(log)}
}

func (e *syncEmitter) Emit(ctx context.Context, ev Event) error {
	if _, err := e.store.Insert(ctx, ev); err != nil {
		e.log.Error("notify: insert failed", "type", ev.Type, "source", ev.Source, "err", err)
		return err
	}
	return nil
}

// AsyncOpts tunes the async emitter.
type AsyncOpts struct {
	// Buffer is the size of the pending-event channel. Defaults to 256.
	Buffer int
}

// asyncEmitter enqueues events to a bounded channel drained by a single worker.
// Emit never blocks: on a full buffer it logs, increments a dropped counter,
// and returns nil. This is best-effort — a crash with buffered events loses
// them. That tradeoff is deliberate: an admin notification must never fail an
// account creation.
type asyncEmitter struct {
	store   inserter
	log     *slog.Logger
	ch      chan Event
	wg      sync.WaitGroup
	dropped atomic.Int64
	closed  atomic.Bool
}

// NewAsyncEmitter starts a background worker and returns the Emitter plus a
// Drain function. Call Drain on shutdown to flush buffered events; it blocks
// until the queue is empty or ctx is done.
func NewAsyncEmitter(store *Store, log *slog.Logger, opts AsyncOpts) (Emitter, func(context.Context) error) {
	buf := opts.Buffer
	if buf <= 0 {
		buf = 256
	}
	e := &asyncEmitter{
		store: store,
		log:   orDefault(log),
		ch:    make(chan Event, buf),
	}
	e.wg.Add(1)
	go e.run()
	return e, e.drain
}

func (e *asyncEmitter) Emit(_ context.Context, ev Event) error {
	// Validate synchronously so a malformed event surfaces immediately as a
	// caller bug, but never block on the write itself.
	if err := ev.normalize(); err != nil {
		return err
	}
	if e.closed.Load() {
		return ErrEmitterClosed
	}
	select {
	case e.ch <- ev:
		return nil
	default:
		n := e.dropped.Add(1)
		e.log.Warn("notify: event dropped, buffer full",
			"type", ev.Type, "source", ev.Source, "dropped_total", n)
		return nil
	}
}

// Dropped reports how many events have been dropped due to a full buffer.
func (e *asyncEmitter) Dropped() int64 { return e.dropped.Load() }

func (e *asyncEmitter) run() {
	defer e.wg.Done()
	for ev := range e.ch {
		ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
		if _, err := e.store.Insert(ctx, ev); err != nil {
			e.log.Error("notify: insert failed",
				"type", ev.Type, "source", ev.Source, "err", err)
		}
		cancel()
	}
}

func (e *asyncEmitter) drain(ctx context.Context) error {
	if !e.closed.CompareAndSwap(false, true) {
		return nil // already draining/drained
	}
	close(e.ch)

	done := make(chan struct{})
	go func() {
		e.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// orDefault returns log or, if nil, the process default logger.
func orDefault(log *slog.Logger) *slog.Logger {
	if log == nil {
		return slog.Default()
	}
	return log
}
