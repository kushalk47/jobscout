package linkedin

import "reflect"

// Selectors is the ONE place that knows LinkedIn's markup.
//
// When LinkedIn changes its UI, this table is the usual fix. Every entry is a
// list of alternatives tried in order, so a new selector can be added in front
// of an old one without breaking older layouts. Users can also override any
// entry from config.yaml (`selectors:` key) without rebuilding the binary.
//
// Three layouts are covered: the signed-in 2026 UI, the older signed-in
// "unified top card" UI, and the public (guest) pages LinkedIn serves when
// the profile is not signed in.
type Selectors struct {
	// Search results page.
	SearchCard       []string `yaml:"search_card" json:"search_card"`                 // one element per job result
	SearchCardIDAttr []string `yaml:"search_card_id_attr" json:"search_card_id_attr"` // attribute holding the job id (digits or urn:li:jobPosting:<digits>)
	SearchTitle      []string `yaml:"search_title" json:"search_title"`
	SearchTitleLink  []string `yaml:"search_title_link" json:"search_title_link"`
	SearchCompany    []string `yaml:"search_company" json:"search_company"`
	SearchLocation   []string `yaml:"search_location" json:"search_location"`
	SearchFooter     []string `yaml:"search_footer" json:"search_footer"` // "3 hours ago", "Easy Apply", "Promoted", salary
	SearchLines      []string `yaml:"search_lines" json:"search_lines"`   // fallback: card lines in order (company, location, badges)
	SearchHeader     []string `yaml:"search_header" json:"search_header"` // "42 results"
	SearchNoResults  []string `yaml:"search_no_results" json:"search_no_results"`

	// Job detail page.
	DetailDescription []string `yaml:"detail_description" json:"detail_description"`
	DetailExpand      []string `yaml:"detail_expand" json:"detail_expand"` // "… more" buttons
	DetailTitle       []string `yaml:"detail_title" json:"detail_title"`
	DetailCompany     []string `yaml:"detail_company" json:"detail_company"`
	DetailLocation    []string `yaml:"detail_location" json:"detail_location"`
	DetailPosted      []string `yaml:"detail_posted" json:"detail_posted"`
	DetailApplicants  []string `yaml:"detail_applicants" json:"detail_applicants"`
	DetailTopCard     []string `yaml:"detail_top_card" json:"detail_top_card"` // block with "location · posted · applicants"
	DetailAboutHeader []string `yaml:"detail_about_header" json:"detail_about_header"`
}

// DefaultSelectors lists the signed-in UI first, then older and guest layouts.
func DefaultSelectors() Selectors {
	return Selectors{
		SearchCard: []string{
			`div[role="button"][componentkey^="job-card-component-ref-"]`, // 2026 signed-in UI
			"li[data-occludable-job-id]",
			"div.job-card-container[data-job-id]",
			"li.jobs-search-results__list-item",
			"div.base-search-card[data-entity-urn]", // guest
		},
		SearchCardIDAttr: []string{"componentkey", "data-occludable-job-id", "data-job-id", "data-entity-urn"},
		SearchTitle: []string{
			`p > span[aria-hidden="true"]`, // 2026 signed-in UI: visible title next to an "(Verified job)" a11y copy
			`a.job-card-list__title--link span[aria-hidden="true"]`,
			"a.job-card-list__title--link",
			".job-card-list__title",
			".artdeco-entity-lockup__title",
			"h3.base-search-card__title", // guest
		},
		SearchTitleLink: []string{"a.job-card-list__title--link", "a.base-card__full-link", `a[href*="/jobs/view/"]`},
		SearchCompany: []string{
			".artdeco-entity-lockup__subtitle",
			".job-card-container__primary-description",
			".job-card-container__company-name",
			"h4.base-search-card__subtitle", // guest
		},
		SearchLocation: []string{
			".artdeco-entity-lockup__caption li",
			".artdeco-entity-lockup__caption",
			".job-card-container__metadata-item",
			".job-search-card__location", // guest
		},
		SearchFooter: []string{
			".job-card-list__footer-wrapper li",
			".job-card-container__footer-item",
			".job-card-container__footer-wrapper li",
			".base-search-card__metadata > *", // guest: time, salary, "Actively hiring"
			"time",
		},
		SearchLines:     []string{"p"},
		SearchHeader:    []string{".scaffold-layout__list-header", ".jobs-search-results-list__subtitle", ".results-context-header", "main header"},
		SearchNoResults: []string{".jobs-search-no-results-banner", ".jobs-search-no-results", ".no-results"},

		DetailDescription: []string{
			`[data-testid="expandable-text-box"]`,
			".jobs-description__content .jobs-box__html-content",
			".jobs-description__content",
			"#job-details",
			".jobs-description",
			".show-more-less-html__markup", // guest
		},
		DetailExpand: []string{
			`[data-testid="expandable-text-button"]`,
			"button.jobs-description__footer-button",
			"button.show-more-less-html__button--more", // guest
		},
		DetailTitle: []string{
			".job-details-jobs-unified-top-card__job-title h1",
			".job-details-jobs-unified-top-card__job-title",
			"h1.top-card-layout__title", // guest
			"h1.t-24",
			"main h1",
		},
		DetailCompany: []string{
			".job-details-jobs-unified-top-card__company-name a",
			".job-details-jobs-unified-top-card__company-name",
			"a.topcard__org-name-link", // guest
			`main a[href*="/company/"]`,
		},
		DetailLocation:   []string{".topcard__flavor--bullet"}, // guest; signed-in uses the "·" line
		DetailPosted:     []string{".posted-time-ago__text"},   // guest
		DetailApplicants: []string{".num-applicants__caption"}, // guest
		DetailTopCard: []string{
			".job-details-jobs-unified-top-card__primary-description-container",
			".job-details-jobs-unified-top-card__tertiary-description-container",
			"#job-top-card",
			".jobs-unified-top-card",
			".top-card-layout__entity-info", // guest
		},
		DetailAboutHeader: []string{"About the job"},
	}
}

// Merge returns s with every non-empty entry of o replacing the default.
func (s Selectors) Merge(o Selectors) Selectors {
	out := s
	ov, dv := reflect.ValueOf(o), reflect.ValueOf(&out).Elem()
	for i := 0; i < ov.NumField(); i++ {
		if f := ov.Field(i); f.Len() > 0 {
			dv.Field(i).Set(f)
		}
	}
	return out
}
