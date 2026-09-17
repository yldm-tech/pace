package main_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEveryPublisherIsRouted is the guard against a task going to a queue nothing consumes.
//
// NewCeleryPublisher falls back to Celery's default queue, which the Python worker used to consume and which nothing has consumed since it was removed. A publisher built without RouteToGoWorker therefore publishes into silence: the request succeeds, the row is written, and the activity, the webhook and the notification that should have followed never happen.
//
// Three of the four publishers in cmd were built that way. This fails if a fourth is.
func TestEveryPublisherIsRouted(t *testing.T) {
	commands, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, command := range commands {
		if !command.IsDir() {
			continue
		}
		entries, err := os.ReadDir(command.Name())
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !strings.HasSuffix(entry.Name(), ".go") {
				continue
			}
			path := filepath.Join(command.Name(), entry.Name())
			contents, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			source := string(contents)
			built := strings.Count(source, "auth.NewCeleryPublisher(")
			if built == 0 {
				continue
			}
			checked += built
			if routed := strings.Count(source, "RouteToGoWorker("); routed < built {
				t.Errorf("%s builds %d publisher(s) and routes %d; an unrouted one publishes to the queue nothing consumes", path, built, routed)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no publishers found, so this guard is not reading the commands")
	}
}
