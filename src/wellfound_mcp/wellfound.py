"""Wellfound page handling.

Everything site-specific lives here, so when Wellfound changes its markup
only this file needs fixing. Parsing is split into pure `parse_*` functions
over page HTML (testable offline) and thin async wrappers that fetch pages.

What the pages look like (checked against the live site, Nov 2026):
- Search pages (/role/<role>, /role/r/<role>, /role/l/<role>/<city>, ?page=N)
  embed a Next.js Apollo cache in __NEXT_DATA__ with JobListingSearchResult
  and StartupResult objects.
- Job pages (/jobs/<id>-<slug>) have no __NEXT_DATA__ but carry a schema.org
  JobPosting in JSON-LD. The slug is required (bare /jobs/<id> is a 404), and
  expired listings return 410.
"""

import html as htmllib
import json
import re
from typing import Any
from urllib.parse import quote

from . import config
from .browser import Browser

NOT_READY = "Not implemented yet: Wellfound page parsing for this step still needs to be written."
DESCRIPTION_CHARS = 1500
SUMMARY_CHARS = 140
PER_PAGE = 20


# ---------- helpers ----------

def _slug(text: str) -> str:
    return quote(re.sub(r"[^a-z0-9]+", "-", text.lower()).strip("-"))


def _text(fragment: str) -> str:
    """HTML fragment -> plain text with paragraph breaks kept."""
    s = re.sub(r"<(br|/p|/li|/h\d|/div)[^>]*>", "\n", fragment, flags=re.I)
    s = re.sub(r"<li[^>]*>", "• ", s, flags=re.I)
    s = htmllib.unescape(re.sub(r"<[^>]+>", "", s))
    s = re.sub(r"[ \t\xa0]+", " ", s)
    return re.sub(r"\n\s*\n+", "\n", s).strip()


def _trim(text: str, n: int) -> str:
    return text if len(text) <= n else text[: n - 1].rsplit(" ", 1)[0] + "…"


def _summary(desc: str) -> str | None:
    """First real sentence of a description, skipping headings like 'About Us'."""
    for line in desc.split("\n"):
        line = re.sub(r"[#*_`>]+", "", line).strip(" •-:")
        if len(line) >= 40:
            return _trim(line, SUMMARY_CHARS)
    return None


def _dedupe(items: list[str]) -> list[str]:
    return list(dict.fromkeys(items))


def _next_data(page_html: str) -> dict[str, Any] | None:
    m = re.search(r'<script id="__NEXT_DATA__"[^>]*>(.*?)</script>', page_html, re.S)
    return json.loads(m.group(1)) if m else None


def _apollo(page_html: str) -> dict[str, Any]:
    data = _next_data(page_html) or {}
    state = data.get("props", {}).get("pageProps", {}).get("apolloState", {})
    return state.get("data", state)


# ---------- login ----------

def parse_logged_in(page_html: str) -> bool | None:
    """True/False from the Apollo viewer on a search page; None if not found."""
    talent = _apollo(page_html).get("ROOT_QUERY", {}).get("talent", {})
    viewer = talent.get("viewer")
    if not isinstance(viewer, dict) or "currentUser" not in viewer:
        return None
    return viewer["currentUser"] is not None


async def is_logged_in(browser: Browser) -> bool:
    page = await browser.goto(f"{config.BASE_URL}/role/r/software-engineer")
    if "/login" in page.url:
        return False
    found = parse_logged_in(await page.content())
    return bool(found)


# ---------- search ----------

def search_url(role: str, location: str | None, remote: bool, page: int = 1) -> str:
    if remote:
        url = f"{config.BASE_URL}/role/r/{_slug(role)}"
    elif location:
        url = f"{config.BASE_URL}/role/l/{_slug(role)}/{_slug(location)}"
    else:
        url = f"{config.BASE_URL}/role/{_slug(role)}"
    return url if page == 1 else f"{url}?page={page}"


def parse_search(page_html: str) -> tuple[list[dict[str, Any]], int]:
    """Return (rows in page order, total job count)."""
    a = _apollo(page_html)
    talent = a.get("ROOT_QUERY", {}).get("talent", {})
    results = next((v for k, v in talent.items() if k.startswith("seoLandingPageJobSearchResults")), None)
    if not results:
        return [], 0

    def deref(ref: Any) -> dict[str, Any]:
        return a.get(ref.get("__ref", ""), {}) if isinstance(ref, dict) else {}

    rows = []
    for s in map(deref, results.get("startups", [])):
        for j in map(deref, s.get("highlightedJobListings", [])):
            if not j.get("id"):
                continue
            desc = _text(j.get("description") or "")
            lo, hi = j.get("yearsExperienceMin"), j.get("yearsExperienceMax")
            rows.append({
                "id": f"{j['id']}-{j.get('slug', '')}".rstrip("-"),
                "title": j.get("title"),
                "company": s.get("name"),
                "location": ", ".join(_dedupe(j.get("locationNames") or [])) or None,
                "remote": j.get("remote"),
                "salary": htmllib.unescape(j.get("compensation") or "") or None,
                "years": f"{lo}+" if lo and not hi else (f"{lo}-{hi}" if hi else None),
                "summary": _summary(desc),
            })
    return rows, int(results.get("totalJobCount") or 0)


async def search_jobs(browser: Browser, role: str, location: str | None, remote: bool, limit: int) -> list[dict[str, Any]]:
    rows: list[dict[str, Any]] = []
    page_no = 1
    while len(rows) < limit:
        page = await browser.goto(search_url(role, location, remote, page_no))
        found, total = parse_search(await page.content())
        rows.extend(found)
        if not found or page_no * PER_PAGE >= total:
            break
        page_no += 1
    return rows[:limit]


# ---------- job detail ----------

def _json_ld(page_html: str) -> dict[str, Any] | None:
    for m in re.finditer(r'<script type="application/ld\+json">(.*?)</script>', page_html, re.S):
        try:
            d = json.loads(m.group(1))
        except json.JSONDecodeError:
            continue
        if isinstance(d, dict) and d.get("@type") == "JobPosting":
            return d
    return None


def _labeled(page_html: str, label: str) -> str | None:
    """Text of the small 'Label / value' blocks in the job sidebar."""
    m = re.search(rf'font-semibold">{re.escape(label)}</span>(.*?)</div>', page_html, re.S)
    return _text(m.group(1)) or None if m else None


def _salary_ld(d: dict[str, Any]) -> str | None:
    b = d.get("baseSalary") or {}
    v = b.get("value") or {}
    lo, hi = v.get("minValue"), v.get("maxValue")
    if lo is None and hi is None:
        return None
    rng = "–".join(f"{x:,.0f}" for x in (lo, hi) if x is not None)
    return f"{b.get('currency', '')} {rng} / {str(v.get('unitText', 'year')).lower()}".strip()


def _place(p: dict[str, Any]) -> str:
    a = p.get("address") or {}
    return ", ".join(x for x in (a.get("addressLocality"), a.get("addressCountry")) if x)


def parse_job(page_html: str, job_id: str) -> dict[str, Any]:
    d = _json_ld(page_html)
    if not d:
        return {"error": "Could not find job data on the page.", "id": job_id}

    header = re.search(r"<h1[^>]*>.*?</h1>\s*<ul[^>]*>\s*<li[^>]*>(.*?)</li>", page_html, re.S)
    comp = _text(header.group(1)) if header else ""
    months = (d.get("experienceRequirements") or {}).get("monthsOfExperience")
    skills_block = re.search(r'font-semibold">Skills</span>(.*?)<h2', page_html, re.S)
    skills = [_text(s) for s in re.findall(r"<div[^>]*rounded-3xl[^>]*>(.*?)</div>", skills_block.group(1))] if skills_block else []

    return {
        "id": job_id,
        "title": d.get("title"),
        "company": (d.get("hiringOrganization") or {}).get("name"),
        "url": f"{config.BASE_URL}/jobs/{job_id}",
        # The header text includes equity ("$150k – $180k • 0.5% – 1.0%"); JSON-LD doesn't.
        "salary": comp if re.search(r"\d", comp) else _salary_ld(d),
        "type": d.get("employmentType"),
        "remote": d.get("jobLocationType") == "TELECOMMUTE",
        "remote_policy": _labeled(page_html, "Remote Work Policy"),
        "locations": _dedupe([x for x in map(_place, d.get("jobLocation") or []) if x]),
        "hires_in": [c.get("name") for c in d.get("applicantLocationRequirements") or [] if c.get("name")],
        "years": round(months / 12, 1) if months else None,
        "visa": _labeled(page_html, "Visa Sponsorship"),
        "relocation": _labeled(page_html, "Relocation"),
        "stack": skills,
        "posted": (d.get("datePosted") or "")[:10] or None,
        "apply_on_wellfound": d.get("directApply"),
        "description": _trim(_text(d.get("description") or ""), DESCRIPTION_CHARS),
    }


async def get_job(browser: Browser, job_id: str) -> dict[str, Any]:
    if not re.fullmatch(r"\d+-[a-z0-9-]+", job_id):
        return {"error": "job_id must be the full id from search_jobs, like '4417481-software-engineer'."}
    page = await browser.goto(f"{config.BASE_URL}/jobs/{job_id}")
    if browser.last_status == 410:
        return {"error": "This job has expired.", "id": job_id}
    if browser.last_status == 404:
        return {"error": "Job not found. Check the id from search_jobs.", "id": job_id}
    return parse_job(await page.content(), job_id)


# ---------- applying (needs a logged-in session; not built yet) ----------

async def application_questions(browser: Browser, job_id: str) -> list[dict[str, Any]]:
    # TODO: needs a logged-in session to inspect. The job page's Apply button
    # goes to /jobs/<id>-<slug>?autoOpenApplication=true, which likely opens
    # the dialog directly. List each field as
    #   {"key", "label", "type": "text|textarea|select|checkbox", "required", "options"}
    raise NotImplementedError(NOT_READY)


async def submit(browser: Browser, job_id: str, answers: dict[str, str]) -> None:
    # TODO: open the apply dialog, fill each field by key, click submit,
    # and confirm the success state appears. Raise if it does not.
    raise NotImplementedError(NOT_READY)
