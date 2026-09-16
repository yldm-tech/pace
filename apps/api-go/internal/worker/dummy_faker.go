package worker

import (
	"strings"
	"time"

	"gorm.io/gorm/clause"
)

// The made-up values the dummy project is filled with.
//
// Upstream uses Faker, and this does not try to be Faker: a name here is not the name Faker would have picked for the same call. What matters is that the values are of the same kind and the same rough size — a person's name, a colour's name, a hex colour, a paragraph of a given length, a date inside this year — because the point of the data is to look like a busy project, not to be any particular project.
var dummyFirstNames = []string{
	"Ada", "Grace", "Alan", "Katherine", "Linus", "Barbara", "Edsger", "Margaret",
	"Donald", "Radia", "Ken", "Frances", "Dennis", "Adele", "Tim", "Sophie",
	"Guido", "Anita", "James", "Karen", "Bjarne", "Shafi", "Vint", "Jean",
}

var dummySurnames = []string{
	"Lovelace", "Hopper", "Turing", "Johnson", "Torvalds", "Liskov", "Dijkstra", "Hamilton",
	"Knuth", "Perlman", "Thompson", "Allen", "Ritchie", "Goldberg", "Berners-Lee", "Wilson",
	"Rossum", "Borg", "Gosling", "Sparck", "Stroustrup", "Goldwasser", "Cerf", "Bartik",
}

var dummyColorNames = []string{
	"Alice Blue", "Antique White", "Aquamarine", "Azure", "Beige", "Bisque", "Blanched Almond",
	"Blue Violet", "Brown", "Burlywood", "Cadet Blue", "Chartreuse", "Chocolate", "Coral",
	"Cornflower Blue", "Cornsilk", "Crimson", "Cyan", "Dark Goldenrod", "Dark Khaki",
	"Dark Olive Green", "Dark Orange", "Dark Orchid", "Dark Salmon", "Dark Sea Green",
	"Dark Slate Blue", "Dark Turquoise", "Deep Pink", "Deep Sky Blue", "Dodger Blue",
	"Firebrick", "Floral White", "Forest Green", "Fuchsia", "Gainsboro", "Gold", "Goldenrod",
	"Honeydew", "Hot Pink", "Indian Red", "Indigo", "Ivory", "Khaki", "Lavender", "Lawn Green",
	"Lemon Chiffon", "Lime Green", "Linen", "Magenta", "Maroon",
}

// dummySentences are the raw material the paragraphs are cut from, the way Faker's lorem is.
var dummySentences = []string{
	"The board is tracking the migration ahead of the release.",
	"Nobody has picked this up since the last planning session.",
	"The first pass is done and the second is waiting on review.",
	"This blocks the export until the schema settles.",
	"Reproduced on staging, not yet on a local install.",
	"The fix is small but the test around it is not.",
	"Waiting on a decision about which of the two shapes to keep.",
	"Split out of a larger piece that was getting hard to read.",
	"The numbers stopped matching after the last backfill.",
	"Somebody should write down why this was done the way it was.",
}

func (tasks *DummyDataTasks) fakeName() string {
	first := dummyFirstNames[tasks.random.Intn(len(dummyFirstNames))]
	last := dummySurnames[tasks.random.Intn(len(dummySurnames))]
	return first + " " + last
}

func (tasks *DummyDataTasks) fakeColorName() string {
	return dummyColorNames[tasks.random.Intn(len(dummyColorNames))]
}

const hexDigits = "0123456789abcdef"

func (tasks *DummyDataTasks) fakeHexColor() string {
	var builder strings.Builder
	builder.WriteByte('#')
	for index := 0; index < 6; index++ {
		builder.WriteByte(hexDigits[tasks.random.Intn(len(hexDigits))])
	}
	return builder.String()
}

// fakeText is fake.text(max_nb_chars=n): sentences joined until adding another would pass the limit.
func (tasks *DummyDataTasks) fakeText(limit int) string {
	var builder strings.Builder
	for {
		sentence := dummySentences[tasks.random.Intn(len(dummySentences))]
		if builder.Len() > 0 {
			if builder.Len()+1+len(sentence) > limit {
				break
			}
			builder.WriteByte(' ')
		} else if len(sentence) > limit {
			return truncateRunes(sentence, limit)
		}
		builder.WriteString(sentence)
	}
	return builder.String()
}

// fakeDateRange is the start and end a cycle, a module or a work item gets. Half of them have no start at all, and the ones that do end somewhere between the start and the end of the year.
func (tasks *DummyDataTasks) fakeDateRange() (any, any) {
	if tasks.random.Intn(2) == 0 {
		// A missing start means a missing end too, which is what the None branch leaves behind.
		return nil, nil
	}
	now := tasks.clock().UTC()
	yearStart := time.Date(now.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
	yearEnd := time.Date(now.Year(), 12, 31, 0, 0, 0, 0, time.UTC)
	span := int(now.Sub(yearStart).Hours() / 24)
	if span < 1 {
		span = 1
	}
	start := yearStart.AddDate(0, 0, tasks.random.Intn(span))
	remaining := int(yearEnd.Sub(start).Hours() / 24)
	if remaining < 1 {
		// The end has to fall strictly after the start, which is the loop upstream spins in until it does.
		return start.Format("2006-01-02"), yearEnd.Format("2006-01-02")
	}
	end := start.AddDate(0, 0, 1+tasks.random.Intn(remaining))
	return start.Format("2006-01-02"), end.Format("2006-01-02")
}

// onConflictDoNothing is bulk_create(ignore_conflicts=True): a row that would collide with one already there is skipped rather than failing the batch.
func onConflictDoNothing() clause.Expression {
	return clause.OnConflict{DoNothing: true}
}
