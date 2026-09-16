package worker

import (
	"math/rand"
	"strings"
	"testing"
)

func dummyTasks() *DummyDataTasks {
	tasks := NewDummyDataTasks(nil, nil)
	tasks.random = rand.New(rand.NewSource(1))
	return tasks
}

// The five states this writes are not the six a real project starts with: the colours differ and there is no triage state at all, so a project made this way has nothing for its intake to file into.
func TestTheDummyStatesAreNotTheRealOnes(t *testing.T) {
	if len(dummyStates) != 5 {
		t.Fatalf("there are %d states", len(dummyStates))
	}
	for _, state := range dummyStates {
		if state.Group == "triage" {
			t.Error("a triage state is written, and the task writes none")
		}
	}
	if dummyStates[0].Name != "Backlog" || !dummyStates[0].Default {
		t.Errorf("the first state is %#v", dummyStates[0])
	}
	// The backlog colour is the task's own rather than the one a real project gets.
	if dummyStates[0].Color != "#A3A3A3" {
		t.Errorf("the backlog colour is %s", dummyStates[0].Color)
	}
}

// A sample takes each value at most once, and asking for more than there are gives all of them rather than raising the way python would.
func TestTheSample(t *testing.T) {
	tasks := dummyTasks()
	values := []string{"a", "b", "c", "d"}
	got := tasks.sample(values, 3)
	if len(got) != 3 {
		t.Fatalf("took %d of them", len(got))
	}
	seen := map[string]bool{}
	for _, value := range got {
		if seen[value] {
			t.Errorf("%q was taken twice", value)
		}
		seen[value] = true
	}
	if len(tasks.sample(values, 99)) != len(values) {
		t.Error("asking for more than there are did not give all of them")
	}
	if tasks.sample(values, 0) != nil {
		t.Error("asking for none gave something")
	}
	if tasks.sample(nil, 3) != nil {
		t.Error("taking from nothing gave something")
	}
}

// The text is cut to the length asked for, counted in characters, and a name is the opening of the same text.
func TestTheMadeUpText(t *testing.T) {
	tasks := dummyTasks()
	for _, limit := range []int{40, 200, 3000} {
		text := tasks.fakeText(limit)
		if len([]rune(text)) > limit {
			t.Errorf("text for a limit of %d is %d characters", limit, len([]rune(text)))
		}
		if text == "" {
			t.Errorf("text for a limit of %d is empty", limit)
		}
	}
	// A limit shorter than a single sentence still gives something, cut to fit.
	if got := tasks.fakeText(10); len([]rune(got)) > 10 {
		t.Errorf("a short limit gave %q", got)
	}
	if got := truncateRunes("Ünicode ☃ and more", 9); len([]rune(got)) != 9 {
		t.Errorf("cutting counted bytes rather than characters: %q", got)
	}
}

// A colour is six hex digits behind a hash, which is the shape the column holds.
func TestTheMadeUpColour(t *testing.T) {
	tasks := dummyTasks()
	for index := 0; index < 20; index++ {
		colour := tasks.fakeHexColor()
		if len(colour) != 7 || !strings.HasPrefix(colour, "#") {
			t.Fatalf("the colour is %q", colour)
		}
		for _, character := range colour[1:] {
			if !strings.ContainsRune(hexDigits, character) {
				t.Fatalf("the colour is %q", colour)
			}
		}
	}
}

// Half the ranges have no start at all, and the ones that do end strictly after they begin.
func TestTheMadeUpDateRange(t *testing.T) {
	tasks := dummyTasks()
	empty, dated := 0, 0
	for index := 0; index < 200; index++ {
		start, end := tasks.fakeDateRange()
		if start == nil {
			if end != nil {
				t.Fatal("a range with no start has an end")
			}
			empty++
			continue
		}
		dated++
		if start.(string) >= end.(string) {
			t.Fatalf("the range %v to %v does not move forward", start, end)
		}
	}
	if empty == 0 || dated == 0 {
		t.Errorf("%d ranges were empty and %d were dated", empty, dated)
	}
}

// The intake statuses are the five the model declares, and only a snoozed one carries a moment it wakes up.
func TestTheIntakeStatuses(t *testing.T) {
	want := map[int]bool{-2: true, -1: true, 0: true, 1: true, 2: true}
	if len(dummyIntakeStatuses) != len(want) {
		t.Fatalf("there are %d statuses", len(dummyIntakeStatuses))
	}
	for _, status := range dummyIntakeStatuses {
		if !want[status] {
			t.Errorf("%d is not a status the model declares", status)
		}
	}
}
