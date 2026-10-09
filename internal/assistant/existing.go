package assistant

import (
	"github.com/ChinmayGit8765/lucidbench/internal/boards"
	"github.com/ChinmayGit8765/lucidbench/internal/ideas"
	"github.com/ChinmayGit8765/lucidbench/internal/memory"
)

// ExistingFrom lists every card on every board and every idea that came
// from a council, for a braindump's "looks like an existing card". Reading
// fails quietly: an unreadable board only means fewer matches.
func ExistingFrom(vault memory.Opener, ideasList func() ([]ideas.Summary, error)) func() []Existing {
	return func() []Existing {
		var out []Existing
		if vault != nil {
			if v, err := vault(); err == nil {
				if list, err := boards.List(v); err == nil {
					for _, bs := range list {
						b, err := boards.Get(v, bs.ID)
						if err != nil {
							continue
						}
						for _, c := range b.Cards {
							out = append(out, Existing{Kind: "card", Title: c.Title, Ref: b.ID + "/" + c.ID})
						}
					}
				}
			}
		}
		if ideasList != nil {
			if list, err := ideasList(); err == nil {
				for _, i := range list {
					if i.Council != "" && i.Title != "" {
						out = append(out, Existing{Kind: "idea", Title: i.Title, Ref: i.ID})
					}
				}
			}
		}
		return out
	}
}
