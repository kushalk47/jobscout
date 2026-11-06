package jobs

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Trimming removes the parts of a job description that never decide
// eligibility (company pitch, perks, EEO boilerplate, "how to apply") and
// keeps everything that does. It is deliberately conservative:
//   - unknown sections are always kept;
//   - eligibility lines (years, degree, visa, location, notice period ...)
//     are rescued even from dropped sections;
//   - if no section headings are recognised, the text is returned whole.

// Trimmed is the result of Trim.
type Trimmed struct {
	Text          string
	Signals       string // "exp 2+y; degree; no visa sponsorship"
	Trimmed       bool   // false when the text came back whole
	OriginalChars int
}

var (
	keepHeading = regexp.MustCompile(`(?i)^about (?:the |this )?(?:job|role|position|opportunity|you)\b`)
	dropHeading = regexp.MustCompile(`(?i)^(about\b(?:\s+(?:us|the\s+(?:company|team|org\w*)|[A-Z][\w&.\- ]{0,40}))?$|who we are|our (?:story|mission|values|culture|commitment)|why (?:join|work|you(?:'|’)ll love|us)|life at|corporate overview|company overview|how we work|benefits?|perks|what we offer|we offer|our offer|equal (?:employment )?opportunit|eeo|diversity|inclusion|accommodation|privacy|how to apply|application process|disclaimer|note to (?:recruiters|agencies)|recruitment fraud|additional information$)`)
	boilerplate = regexp.MustCompile(`(?i)equal opportunity employer|regardless of (?:race|age|gender|religion)|without regard to (?:race|age|gender|religion)|reasonable accommodation|e-?verify|privacy (?:notice|policy)|we do not accept unsolicited`)
	eligibility = regexp.MustCompile(`(?i)\b\d+\s*\+?\s*(?:-|–|to)?\s*\d*\s*\+?\s*(?:years?|yrs?)\b|\bexperience\b|\bdegree\b|\bbachelor|\bmaster|\bb\.?\s?tech\b|\bm\.?\s?tech\b|\bph\.?d\b|\bvisa\b|\bsponsor|\bauthori[sz]ed to work\b|\bwork authori[sz]ation\b|\bcitizen|\bclearance\b|\brelocat|\bnotice period\b|\bon-?site\b|\bhybrid\b|\bremote\b|\bmust\b|\brequired\b|\bsalary\b|\bcompensation\b|\bctc\b|\blpa\b`)
	expRange    = regexp.MustCompile(`(?i)\b(\d{1,2})\s*(\+|plus\b|or more\b)?\s*(?:(?:-|–|to)\s*(\d{1,2})\s*\+?\s*)?(years?|yrs?|months?)\b`)
	noSponsor   = regexp.MustCompile(`(?i)(?:not?|unable to|cannot|can't|won't)\s+(?:\w+\s+){0,3}sponsor|sponsorship (?:is )?not (?:available|offered|provided)`)
	sponsorship = regexp.MustCompile(`(?i)\bsponsor`)
	degreeWord  = regexp.MustCompile(`(?i)\b(?:bachelor|master|b\.?\s?tech|m\.?\s?tech|b\.?e\.?|ph\.?d|degree)\b`)
)

const introCap = 200

// Trim compacts a structured description (lines from linkedin.StructuredText).
// maxChars caps the output; 0 means no cap.
func Trim(desc string, maxChars int) Trimmed {
	out := Trimmed{OriginalChars: len(desc), Signals: signals(desc)}
	lines := strings.Split(desc, "\n")

	type section struct {
		heading string
		lines   []string
	}
	var secs []section
	cur := section{}
	headings := 0
	for _, raw := range lines {
		l := strings.TrimSpace(raw)
		if l == "" {
			continue
		}
		if h, ok := asHeading(l); ok {
			secs = append(secs, cur)
			cur = section{heading: h}
			headings++
			continue
		}
		cur.lines = append(cur.lines, l)
	}
	secs = append(secs, cur)

	var b strings.Builder
	var rescued []string
	dropped := false
	for i, s := range secs {
		drop := s.heading != "" && dropHeading.MatchString(s.heading) && !keepHeading.MatchString(s.heading)
		if drop {
			dropped = true
			for _, l := range s.lines {
				if eligibility.MatchString(l) && !boilerplate.MatchString(l) {
					rescued = append(rescued, l)
				}
			}
			continue
		}
		if s.heading != "" {
			fmt.Fprintf(&b, "## %s\n", s.heading)
		}
		body := make([]string, 0, len(s.lines))
		for _, l := range s.lines {
			if boilerplate.MatchString(l) && !eligibility.MatchString(l) {
				dropped = true
				continue
			}
			body = append(body, l)
		}
		if i == 0 && headings > 0 { // intro before the first heading: usually a company pitch
			intro := strings.Join(body, " ")
			if len(intro) > introCap {
				intro = cutAt(intro, introCap) + " …"
				dropped = true
			}
			if intro != "" {
				b.WriteString(intro + "\n")
			}
			continue
		}
		for _, l := range body {
			b.WriteString(l + "\n")
		}
	}
	if len(rescued) > 0 {
		b.WriteString("## Other eligibility notes\n")
		for _, l := range rescued {
			b.WriteString(l + "\n")
		}
	}
	out.Text = strings.TrimSpace(b.String())
	out.Trimmed = dropped
	if maxChars > 0 && len(out.Text) > maxChars {
		out.Text = cutAt(out.Text, maxChars) + "\n… [cut at " + fmt.Sprint(maxChars) + " chars; use full=true]"
		out.Trimmed = true
	}
	return out
}

// knownHeading matches common section titles written as plain lines (no bold, no colon).
var knownHeading = regexp.MustCompile(`(?i)^(?:(?:key |your |main |core |primary )?responsibilities|(?:required |minimum |basic |preferred |desired )?(?:qualifications|skills|requirements|experience)(?: and (?:skills|experience|attributes))?|preferred skills|nice to have|good to have|what you(?:'|’)ll do|what you will do|what we(?:'|’)re looking for|what we are looking for|who you are|about (?:the )?(?:role|job|position|team|company|us)|about [A-Z][\w&.\- ]{0,30}|corporate overview|company overview|job (?:summary|description|overview)|role overview|the role|your role|this role|why join us|why (?:you(?:'|’)ll love|work) .{0,30}|benefits|perks(?: and benefits)?|what we offer|our (?:mission|values|culture|story)|how we work|equal opportunity.*|eeo statement)$`)

// asHeading recognises "## X" lines, short plain lines ending in ':', and
// common section titles on their own line.
func asHeading(l string) (string, bool) {
	if strings.HasPrefix(l, "## ") {
		return strings.TrimSpace(strings.TrimPrefix(l, "## ")), true
	}
	if len(l) <= 60 && strings.HasSuffix(l, ":") && !strings.HasPrefix(l, "- ") {
		return strings.TrimSuffix(l, ":"), true
	}
	if len(l) <= 50 && knownHeading.MatchString(strings.TrimSpace(l)) {
		return strings.TrimSpace(l), true
	}
	return "", false
}

// cutAt cuts s to at most n bytes, at a sentence or word boundary, never inside a rune.
func cutAt(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := s[:n]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	if i := strings.LastIndexAny(cut, ".!?\n"); i > n/2 {
		return cut[:i+1]
	}
	if i := strings.LastIndexByte(cut, ' '); i > n/2 {
		return cut[:i]
	}
	return cut
}

// signals pulls the few facts that most often decide eligibility into one line.
func signals(desc string) string {
	var parts []string
	seen := map[string]bool{}
	var exps []string
	for _, m := range expRange.FindAllStringSubmatch(desc, -1) {
		e := m[1]
		if m[3] != "" {
			e += "-" + m[3]
		} else if m[2] != "" {
			e += "+"
		}
		if strings.HasPrefix(strings.ToLower(m[4]), "m") {
			e += "mo"
		} else {
			e += "y"
		}
		if !seen[e] && m[1] != "0" {
			seen[e] = true
			exps = append(exps, e)
		}
		if len(exps) == 3 {
			break
		}
	}
	if len(exps) > 0 {
		parts = append(parts, "exp "+strings.Join(exps, ","))
	}
	if degreeWord.MatchString(desc) {
		parts = append(parts, "degree mentioned")
	}
	switch {
	case noSponsor.MatchString(desc):
		parts = append(parts, "NO visa sponsorship")
	case sponsorship.MatchString(desc):
		parts = append(parts, "sponsorship mentioned")
	}
	return strings.Join(parts, "; ")
}
