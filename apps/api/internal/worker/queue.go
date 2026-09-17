package worker

import (
	"os"
	"strings"
)

// Queue is the queue the API and the beat publish to and the worker consumes.
//
// PACE_WORKER_QUEUE overrides it, which is how a deployment can move the three onto a queue of its own. What it may not do is resolve to nothing: the publisher's fallback is the queue Celery uses by default, the Python worker that consumed it is gone, and a publisher that reaches that fallback puts every background task somewhere no consumer is listening. That failed silently — the API answered 201, the row was written, and the activity, the notification and the webhook that should have followed never happened.
func Queue() string {
	if queue := strings.TrimSpace(os.Getenv("PACE_WORKER_QUEUE")); queue != "" {
		return queue
	}
	return DefaultQueue
}
