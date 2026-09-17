package worker

import "testing"

// TestTheQueueNeverFallsBackToCelery pins the thing that failed silently.
//
// The publisher's own fallback is Celery's default queue, which the Python worker used to consume. That worker is gone, so a publisher reaching that fallback puts every background task somewhere nothing is listening — and nothing complains: the request succeeds, the row is written, and the activity, the notification and the webhook that should have followed never happen. Thirty-eight of them were found sitting in that queue with no consumer.
func TestTheQueueNeverFallsBackToCelery(t *testing.T) {
	for _, test := range []struct{ name, set, want string }{
		{name: "unset", set: "", want: DefaultQueue},
		{name: "blank", set: "   ", want: DefaultQueue},
		{name: "named", set: "another-queue", want: "another-queue"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("PACE_WORKER_QUEUE", test.set)
			if got := Queue(); got != test.want {
				t.Fatalf("Queue() = %q, want %q", got, test.want)
			}
			if got := Queue(); got == "celery" {
				t.Fatal("the queue resolved to the one nothing consumes")
			}
		})
	}
}
