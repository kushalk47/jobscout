// Package jobs holds the domain model and the use cases (search, get, mark, list).
// It knows nothing about LinkedIn's HTML, Chrome or SQLite: those live behind the
// Source and Store interfaces so a LinkedIn UI change never reaches this package.
package jobs

import (
	"context"
	"time"
)

// Card is one search result, as small as possible: it is what Claude screens
// by title/company/location before paying for a full description.
type Card struct {
	ID        string
	Title     string
	Company   string
	Location  string
	Workplace string // Remote | Hybrid | On-site | ""
	Posted    string // "3 hours ago" as LinkedIn shows it
	EasyApply bool
	Promoted  bool
	Salary    string
}

// Detail is a job description plus the top-card metadata.
type Detail struct {
	ID          string
	Title       string
	Company     string
	Location    string
	Workplace   string
	Employment  string // Full-time | Contract | ...
	Posted      string
	Applicants  string
	Description string // structured plain text ("## Heading", "- bullet")
	FetchedAt   time.Time
}

// Status is the user's own state for a job. It never touches LinkedIn.
type Status string

const (
	StatusSkipped       Status = "skipped"        // screened out by title, never read
	StatusSeen          Status = "seen"           // read, scored, not shortlisted yet
	StatusShortlisted   Status = "shortlisted"    // in the top list
	StatusApplied       Status = "applied"        // user applied
	StatusRejected      Status = "rejected"       // company said no
	StatusNotInterested Status = "not_interested" // user said no
)

var validStatuses = map[Status]bool{
	StatusSkipped: true, StatusSeen: true, StatusShortlisted: true,
	StatusApplied: true, StatusRejected: true, StatusNotInterested: true,
}

// ValidStatus reports whether s is a known status.
func ValidStatus(s Status) bool { return validStatuses[s] }

// Mark is one evaluation recorded by Claude (or the user).
type Mark struct {
	ID        string
	Status    Status
	Score     *int // 0-100, nil when not scored
	Reason    string
	UpdatedAt time.Time
}

// ListedJob is a marked job joined with its card, for jobs_list.
type ListedJob struct {
	Card
	Mark
}

// SearchQuery is one keyword search sent to the source.
type SearchQuery struct {
	Keywords      string
	Location      string
	PostedWithinH int      // 0 = any time
	Workplace     []string // remote | hybrid | onsite
	Experience    []string // internship | entry | associate | mid_senior | director | executive
	JobType       []string // full_time | part_time | contract | temporary | internship
	EasyApplyOnly bool
	MostRecent    bool
	MaxResults    int
}

// Source fetches jobs from somewhere (LinkedIn via Chrome in production, a fake in tests).
type Source interface {
	Search(ctx context.Context, q SearchQuery) ([]Card, error)
	Get(ctx context.Context, id string) (Detail, error)
}

// Store persists cards, cached details and marks locally.
type Store interface {
	UpsertCards(ctx context.Context, cards []Card) error
	MarkedIDs(ctx context.Context) (map[string]Status, error)
	GetDetail(ctx context.Context, id string, maxAge time.Duration) (*Detail, error)
	SaveDetail(ctx context.Context, d Detail) error
	SaveMarks(ctx context.Context, marks []Mark) error
	List(ctx context.Context, f ListFilter) ([]ListedJob, error)
}

// ListFilter selects marked jobs for jobs_list.
type ListFilter struct {
	Statuses  []Status
	MinScore  int
	SinceDays int
	Limit     int
}
