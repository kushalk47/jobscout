package jobs

import (
	"fmt"
	"regexp"
	"strings"
)

// Compact text output. Every tool answers in pipe-separated lines with one
// header row, never JSON: field names are written once, not once per job.

var agoPattern = regexp.MustCompile(`(?i)(\d+)?\s*(minute|hour|day|week|month|year)s?\s+ago`)

// ShortAgo turns "3 hours ago" into "3h", "Reposted 2 weeks ago" into "2w".
func ShortAgo(s string) string {
	m := agoPattern.FindStringSubmatch(s)
	if m == nil {
		if strings.EqualFold(strings.TrimSpace(s), "just now") {
			return "0h"
		}
		return s
	}
	n := m[1]
	if n == "" {
		n = "1"
	}
	unit := map[string]string{"minute": "m", "hour": "h", "day": "d", "week": "w", "month": "mo", "year": "y"}[strings.ToLower(m[2])]
	return n + unit
}

// ShortLocation keeps city and country: "Bengaluru, Karnataka, India" -> "Bengaluru, India".
func ShortLocation(loc, workplace string) string {
	parts := strings.Split(loc, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	if len(parts) >= 3 {
		loc = parts[0] + ", " + parts[len(parts)-1]
	}
	if workplace != "" {
		loc += " (" + workplace + ")"
	}
	return loc
}

func cell(s string) string {
	return strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(s, "|", "/"), "\n", " "))
}

// FormatCards renders search results.
func FormatCards(cards []Card) string {
	var b strings.Builder
	b.WriteString("id|title|company|location|posted|flags\n")
	for _, c := range cards {
		var flags []string
		if c.EasyApply {
			flags = append(flags, "easy")
		}
		if c.Promoted {
			flags = append(flags, "promo")
		}
		if c.Salary != "" {
			flags = append(flags, cell(c.Salary))
		}
		fmt.Fprintf(&b, "%s|%s|%s|%s|%s|%s\n", c.ID, cell(c.Title), cell(c.Company),
			cell(ShortLocation(c.Location, c.Workplace)), ShortAgo(c.Posted), strings.Join(flags, ","))
	}
	return b.String()
}

// FormatDetail renders one job for eligibility analysis.
func FormatDetail(d Detail, t Trimmed, full bool) string {
	var b strings.Builder
	head := []string{d.ID, cell(d.Title), cell(d.Company), cell(ShortLocation(d.Location, d.Workplace))}
	for _, extra := range []string{d.Employment, ShortAgo(d.Posted), d.Applicants} {
		if extra != "" {
			head = append(head, cell(extra))
		}
	}
	fmt.Fprintf(&b, "=== %s\n", strings.Join(head, " | "))
	if t.Signals != "" {
		fmt.Fprintf(&b, "signals: %s\n", t.Signals)
	}
	if d.Apply != "" {
		fmt.Fprintf(&b, "apply: %s\n", d.Apply)
	}
	if full {
		b.WriteString(d.Description)
	} else {
		b.WriteString(t.Text)
		if t.Trimmed {
			fmt.Fprintf(&b, "\n(trimmed %d→%d chars; full=true for the original)", t.OriginalChars, len(t.Text))
		}
	}
	b.WriteString("\n")
	return b.String()
}

// FormatList renders marked jobs, best first.
func FormatList(items []ListedJob) string {
	var b strings.Builder
	b.WriteString("score|id|title|company|location|status|reason\n")
	for _, j := range items {
		score := "-"
		if j.Score != nil {
			score = fmt.Sprint(*j.Score)
		}
		fmt.Fprintf(&b, "%s|%s|%s|%s|%s|%s|%s\n", score, j.Card.ID, cell(j.Title), cell(j.Company),
			cell(ShortLocation(j.Location, j.Workplace)), j.Status, cell(j.Reason))
	}
	return b.String()
}
