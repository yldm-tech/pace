package worker

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// deliveryFor builds what the broker hands over for one task: the headers that name it and a protocol v2 body. The delivery has no acknowledger, which is what makes the ack a no-op.
func deliveryFor(taskName, issueID string) amqp.Delivery {
	return amqp.Delivery{
		Headers: amqp.Table{"task": taskName, "id": issueID},
		Body:    []byte(fmt.Sprintf(`[[], {"issue_id": %q}, {}]`, issueID)),
	}
}

// Two tasks naming the same work item go to the same lane, which is what keeps them in the order the broker delivered them: the version tasks and the activity task read a row and write it back without a lock, so one of the two writes is lost if they run at once.
func TestTasksNamingTheSameThingShareALane(t *testing.T) {
	first := task{name: IssueActivityTask, keywords: map[string]any{"issue_id": "b1cbd4b4-0f9c-4f4f-9d0c-0f1d2c3b4a59"}}
	second := task{name: IssueDescriptionVersionTask, keywords: map[string]any{"issue_id": "b1cbd4b4-0f9c-4f4f-9d0c-0f1d2c3b4a59"}}
	if laneFor(first, 4) != laneFor(second, 4) {
		t.Error("two tasks on one work item were put on different lanes")
	}

	// A task that names nothing recognised falls back to its own name, so two sweeps never overlap while a sweep and a work item's activity do.
	sweep := task{name: ArchiveAndCloseTask, keywords: map[string]any{}}
	if laneFor(sweep, 4) != laneFor(task{name: ArchiveAndCloseTask, keywords: map[string]any{"batch_size": 100}}, 4) {
		t.Error("two runs of the sweep were put on different lanes")
	}
}

// Every lane is used, so the pool really is as wide as the prefetch rather than one goroutine wearing several names.
func TestTheLanesSpreadAcrossTheWholePool(t *testing.T) {
	seen := map[int]bool{}
	for _, id := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l"} {
		seen[laneFor(task{name: IssueActivityTask, keywords: map[string]any{"issue_id": id}}, 4)] = true
	}
	if len(seen) != 4 {
		t.Errorf("the twelve work items reached %d of the 4 lanes", len(seen))
	}
}

// The pool runs tasks on different things at the same time, which is what the serial loop did not, and finishes the ones already started when it is stopped.
func TestThePoolRunsDifferentThingsAtOnce(t *testing.T) {
	// The two identifiers have to fall on different lanes for the test to be able to tell parallel from serial at all.
	if laneFor(task{keywords: map[string]any{"issue_id": "one"}}, 4) == laneFor(task{keywords: map[string]any{"issue_id": "two"}}, 4) {
		t.Fatal(`"one" and "two" share a lane`)
	}
	consumer := NewConsumer("", "", nil)
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	var done atomic.Int32
	consumer.Register("test.concurrent", func(context.Context, []any, map[string]any) error {
		started <- struct{}{}
		<-release
		done.Add(1)
		return nil
	})

	pool := consumer.startLanes(context.Background())
	for _, id := range []string{"one", "two"} {
		ready, dispatch := consumer.prepare(deliveryFor("test.concurrent", id))
		if !dispatch {
			t.Fatalf("the task for %s was not dispatched", id)
		}
		if !pool.send(context.Background(), ready) {
			t.Fatalf("the task for %s was not sent", id)
		}
	}
	// Both have to be inside the handler before either is let go, which cannot happen if the lanes run one task after another.
	for count := 0; count < 2; count++ {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("only one task was running at a time")
		}
	}
	close(release)
	pool.stop()
	if got := done.Load(); got != 2 {
		t.Errorf("%d of the 2 tasks finished before the pool stopped", got)
	}
}

// A task still queued behind another on its lane when the pool stops is handed back to the broker rather than run against a context that is already cancelled, which is what the serial loop left to the broker's own redelivery.
func TestATaskStillQueuedWhenThePoolStopsIsGivenBack(t *testing.T) {
	consumer := NewConsumer("", "", nil)
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	var ran atomic.Int32
	consumer.Register("test.blocking", func(context.Context, []any, map[string]any) error {
		ran.Add(1)
		entered <- struct{}{}
		<-release
		return nil
	})

	pool := consumer.startLanes(context.Background())
	// The same identifier twice, so the second waits on the lane the first is running on.
	for count := 0; count < 2; count++ {
		ready, dispatch := consumer.prepare(deliveryFor("test.blocking", "the-same-work-item"))
		if !dispatch {
			t.Fatal("the task was not dispatched")
		}
		if !pool.send(context.Background(), ready) {
			t.Fatal("the task was not sent")
		}
	}
	<-entered

	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		pool.stop()
	}()
	// stop marks the pool draining before it closes the lanes; the first task is let go only once that has happened, so the lane's choice about the second one is not a race.
	deadline := time.Now().Add(5 * time.Second)
	for !pool.draining.Load() {
		if time.Now().After(deadline) {
			t.Fatal("the pool never started draining")
		}
		time.Sleep(time.Millisecond)
	}
	close(release)
	<-stopped

	if got := ran.Load(); got != 1 {
		t.Errorf("%d tasks ran, so the queued one was run rather than given back", got)
	}
}
