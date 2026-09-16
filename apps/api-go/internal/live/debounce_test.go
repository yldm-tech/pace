package live

import (
	"sync"
	"testing"
	"time"
)

// TestDebounceDefersAndKeepsDeferring covers the ordinary case: more work arriving pushes the deadline out.
func TestDebounceDefersAndKeepsDeferring(t *testing.T) {
	d := newDebouncer()
	defer d.Stop()

	var mu sync.Mutex
	runs := 0
	work := func() {
		mu.Lock()
		runs++
		mu.Unlock()
	}

	for i := 0; i < 5; i++ {
		d.Debounce("a-page", work, 40*time.Millisecond, time.Second)
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	if runs != 0 {
		t.Errorf("the work ran %d times while it was still being deferred", runs)
	}
	mu.Unlock()

	time.Sleep(120 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if runs != 1 {
		t.Errorf("runs = %d, want one", runs)
	}
}

// TestDebounceHonoursTheLimit is the part that matters most: somebody typing continuously would otherwise defer the save forever.
func TestDebounceHonoursTheLimit(t *testing.T) {
	d := newDebouncer()
	defer d.Stop()

	done := make(chan struct{}, 1)
	work := func() { done <- struct{}{} }

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		d.Debounce("a-page", work, 500*time.Millisecond, 100*time.Millisecond)
		select {
		case <-done:
			return
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the work never ran, although the limit was reached several times over")
}

// TestDebounceRunsAtOnceWithNoWait covers the zero wait, which runs rather than schedules.
func TestDebounceRunsAtOnceWithNoWait(t *testing.T) {
	d := newDebouncer()
	defer d.Stop()

	ran := false
	d.Debounce("a-page", func() { ran = true }, 0, time.Second)
	if !ran {
		t.Error("the work did not run")
	}
}

// TestCancelTakesTheWorkAway covers the caller that is doing the work itself.
func TestCancelTakesTheWorkAway(t *testing.T) {
	d := newDebouncer()
	defer d.Stop()

	ran := false
	d.Debounce("a-page", func() { ran = true }, 20*time.Millisecond, time.Second)
	d.Cancel("a-page")
	time.Sleep(60 * time.Millisecond)
	if ran {
		t.Error("cancelled work ran anyway")
	}
}

// TestStopCancelsEverything covers shutdown.
func TestStopCancelsEverything(t *testing.T) {
	d := newDebouncer()
	ran := false
	d.Debounce("one", func() { ran = true }, 20*time.Millisecond, time.Second)
	d.Debounce("two", func() { ran = true }, 20*time.Millisecond, time.Second)
	d.Stop()
	time.Sleep(60 * time.Millisecond)
	if ran {
		t.Error("work ran after everything was stopped")
	}
}
