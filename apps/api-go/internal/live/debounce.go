package live

import (
	"sync"
	"time"
)

// debouncer defers a piece of work and keeps deferring it while more work arrives, up to a limit.
//
// It is a port of the one the service it replaces uses, and the limit is the part that matters: the deadline is measured from the first call of a batch, so somebody typing continuously has their page saved on a fixed cadence rather than never. A call made at or past that deadline runs immediately rather than scheduling anything.
type debouncer struct {
	mu     sync.Mutex
	timers map[string]*debounced
	clock  func() time.Time
}

type debounced struct {
	start time.Time
	timer *time.Timer
	run   func()
}

func newDebouncer() *debouncer {
	return &debouncer{timers: map[string]*debounced{}, clock: time.Now}
}

// Debounce schedules work under an id, replacing whatever was scheduled under it. A wait of zero runs the work now, and so does a call made once maxWait has passed since the batch started.
func (d *debouncer) Debounce(id string, work func(), wait, maxWait time.Duration) {
	d.mu.Lock()

	start := d.clock()
	if existing, ok := d.timers[id]; ok {
		start = existing.start
		existing.timer.Stop()
	}

	run := func() {
		d.mu.Lock()
		delete(d.timers, id)
		d.mu.Unlock()
		work()
	}

	if wait == 0 || d.clock().Sub(start) >= maxWait {
		delete(d.timers, id)
		d.mu.Unlock()
		work()
		return
	}

	d.timers[id] = &debounced{start: start, timer: time.AfterFunc(wait, run), run: run}
	d.mu.Unlock()
}

// Cancel stops whatever is scheduled under the id without running it, for a caller that is taking the work over itself.
func (d *debouncer) Cancel(id string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if existing, ok := d.timers[id]; ok {
		existing.timer.Stop()
		delete(d.timers, id)
	}
}

// Stop cancels everything scheduled, for shutdown.
func (d *debouncer) Stop() {
	d.mu.Lock()
	defer d.mu.Unlock()
	for id, existing := range d.timers {
		existing.timer.Stop()
		delete(d.timers, id)
	}
}
