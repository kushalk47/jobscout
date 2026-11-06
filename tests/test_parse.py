"""Offline tests for the page parsers, using tiny pages shaped like Wellfound's
(structure copied from the live site, Nov 2026)."""

import json

from wellfound_mcp import wellfound as w


def search_page(viewer_user=None) -> str:
    apollo = {
        "ROOT_QUERY": {"talent": {
            "viewer": {"currentUser": viewer_user},
            'seoLandingPageJobSearchResults({"page":1,"role":"x"})': {
                "totalJobCount": 2, "startups": [{"__ref": "StartupResult:1"}],
            },
        }},
        "StartupResult:1": {"name": "Acme", "highlightedJobListings": [
            {"__ref": "JobListingSearchResult:10"}, {"__ref": "JobListingSearchResult:11"}]},
        "JobListingSearchResult:10": {
            "id": "10", "slug": "backend-engineer", "title": "Backend Engineer",
            "locationNames": ["Bengaluru", "Bengaluru"], "remote": False,
            "compensation": "$100k – $120k &amp; more", "yearsExperienceMin": 3, "yearsExperienceMax": None,
            "description": "## **About Us**\n\nWe build delightful developer tools for small teams everywhere.",
        },
        "JobListingSearchResult:11": {"id": "11", "slug": "sre", "title": "SRE", "yearsExperienceMin": 2, "yearsExperienceMax": 5},
    }
    nd = {"props": {"pageProps": {"apolloState": {"data": apollo}}}}
    return f'<html><script id="__NEXT_DATA__" type="application/json">{json.dumps(nd)}</script></html>'


JOB_PAGE = """<html><script type="application/ld+json">{ld}</script>
<h1 class="x">Backend Engineer</h1><ul class="y"><li class="z">$150k – $180k • 0.5% – 1.0%</li><li>Remote</li></ul>
<div><span class="text-md font-semibold">Visa Sponsorship</span><p><span>Not Available</span></p></div>
<div><span class="text-md font-semibold">Skills</span><div class="flex flex-wrap">
<div class="mr-2 rounded-3xl border">Python</div><div class="mr-2 rounded-3xl border">Go &amp; gRPC</div></div></div></div>
<h2>About the job</h2></html>""".format(ld=json.dumps({
    "@type": "JobPosting", "title": "Backend Engineer", "hiringOrganization": {"name": "Acme"},
    "employmentType": "FULL_TIME", "jobLocationType": "TELECOMMUTE", "datePosted": "2026-07-01T15:49:14Z",
    "jobLocation": [{"address": {"addressLocality": "Berlin", "addressCountry": "Germany"}}],
    "experienceRequirements": {"monthsOfExperience": 36}, "directApply": True,
    "description": "<p>About us</p><ul><li>Fast &amp; small</li></ul>" + "<p>" + "word " * 500 + "</p>",
}))


def test_parse_search():
    rows, total = w.parse_search(search_page())
    assert total == 2
    assert [r["id"] for r in rows] == ["10-backend-engineer", "11-sre"]
    r = rows[0]
    assert r["company"] == "Acme" and r["location"] == "Bengaluru"
    assert r["salary"] == "$100k – $120k & more"
    assert r["years"] == "3+" and rows[1]["years"] == "2-5"
    assert r["summary"].startswith("We build delightful")


def test_logged_in():
    assert w.parse_logged_in(search_page()) is False
    assert w.parse_logged_in(search_page({"id": "1"})) is True
    assert w.parse_logged_in("<html></html>") is None


def test_search_url():
    assert w.search_url("Software Engineer", None, True) == "https://wellfound.com/role/r/software-engineer"
    assert w.search_url("ML Engineer", "New York", False, 2) == "https://wellfound.com/role/l/ml-engineer/new-york?page=2"
    assert w.search_url("C++ Dev", None, False) == "https://wellfound.com/role/c-dev"


def test_parse_job():
    j = w.parse_job(JOB_PAGE, "10-backend-engineer")
    assert j["company"] == "Acme" and j["remote"] is True
    assert j["salary"] == "$150k – $180k • 0.5% – 1.0%"
    assert j["stack"] == ["Python", "Go & gRPC"]
    assert j["visa"] == "Not Available" and j["years"] == 3.0
    assert j["locations"] == ["Berlin, Germany"] and j["posted"] == "2026-07-01"
    assert "• Fast & small" in j["description"]
    assert len(j["description"]) <= w.DESCRIPTION_CHARS


def test_parse_job_missing():
    assert "error" in w.parse_job("<html></html>", "x")
