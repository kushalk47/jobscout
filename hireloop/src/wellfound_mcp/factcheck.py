"""Rules that decide what may be sent without the user looking first.

fit(job)         - is this job one we may apply to at all?
check(...)       - is every checkable claim in the answers backed by profile.md?

Both are deliberately strict: anything they can't verify is a reason to hold
the application for the user, never a reason to send it.

What check() verifies: numbers, capitalised names (employers, projects, tools,
places), links and emails all appear in the profile or the job posting; no
placeholder text; required questions answered; answers not too long.
What it can't verify: whether true facts are combined correctly (e.g. the
right project credited to the right employer). That part is Claude's review.
"""

import hashlib
import json
import re
from typing import Any

from . import config

INDIA_PLACES = {
    "india", "bengaluru", "bangalore", "bangalore urban", "mumbai", "delhi", "new delhi", "gurugram",
    "gurgaon", "noida", "hyderabad", "chennai", "pune", "kolkata", "ahmedabad", "jaipur", "kochi",
    "chandigarh", "indore", "coimbatore", "thiruvananthapuram", "mysore", "mysuru", "gandhinagar",
}
MAX_ANSWER_CHARS = 1500
PLACEHOLDER = re.compile(r"\[[^\]]*\]|\{[^}]*\}|<[^>]*>|\bTODO\b|\bTBD\b|\bXXX\b|lorem ipsum", re.I)
NUMBER = re.compile(r"(?<![\w.])\d[\d,]*(?:\.\d+)?")
URL = re.compile(r"(?:https?://)?(?:[\w-]+\.)+(?:com|ai|io|in|org|dev|app|co|me|space|net)(?:/[^\s)]*)?", re.I)
EMAIL = re.compile(r"[\w.+-]+@[\w-]+\.[\w.]+")
# A word that starts on its own: "30M-parameter" is a number, not the name "M-parameter".
WORD = re.compile(r"(?<![\w.~-])[A-Za-z][A-Za-z0-9+#&./-]*")
# Capitalised words that are ordinary English, not claims.
COMMON = {
    "i", "i'm", "i've", "i'd", "i'll", "my", "me", "the", "a", "an", "this", "that", "these", "those", "at",
    "in", "on", "for", "with", "and", "but", "or", "as", "to", "of", "from", "by", "it", "its", "we", "you",
    "your", "our", "they", "their", "he", "she", "what", "why", "how", "when", "where", "which", "who",
    "also", "most", "more", "both", "each", "every", "here", "there", "over", "after", "before", "during",
    "while", "since", "because", "so", "if", "then", "than", "be", "am", "is", "are", "was", "were", "have",
    "has", "had", "do", "does", "did", "can", "could", "would", "should", "will", "may", "might", "not",
    "no", "yes", "thank", "thanks", "hi", "hello", "dear", "best", "regards", "sincerely", "currently",
    "recently", "previously", "today", "beyond", "outside", "across", "through", "about", "into", "like",
    "earlier", "later", "now", "finally", "first", "next", "together", "overall", "ideally", "happy",
}


def _years(value: Any) -> float | None:
    if value is None or value == "":
        return None
    m = re.search(r"\d+(?:\.\d+)?", str(value))
    return float(m.group()) if m else None


def _max_lakhs(salary: str | None) -> float | None:
    """Upper bound of an INR salary like '₹4L – ₹8L' in lakhs; None if not INR."""
    if not salary or "₹" not in salary:
        return None
    vals = [float(x) for x in re.findall(r"₹\s*(\d+(?:\.\d+)?)\s*L", salary)]
    return max(vals) if vals else None


def fit(job: dict[str, Any]) -> dict[str, Any]:
    """Whether a job is one we may apply to under the user's rules."""
    if not job or "error" in job:
        return {"ok": False, "reasons": ["Job details unavailable"]}
    reasons = []
    yrs = _years(job.get("years"))
    if yrs is None:
        # Some postings only state experience in the text: "3+ years" or a range like
        # "4–6 years". The lower bound of each is the minimum they ask for.
        text = job.get("description") or ""
        found = [float(n) for n in re.findall(r"(\d+(?:\.\d+)?)\s*\+\s*(?:years|yrs)", text, re.I)]
        found += [float(n) for n in re.findall(r"(\d+(?:\.\d+)?)\s*(?:-|–|—|to)\s*\d+(?:\.\d+)?\s*\+?\s*(?:years|yrs)", text, re.I)]
        yrs = max(found) if found else None
    if yrs is not None and yrs > config.MAX_YEARS:
        reasons.append(f"Needs {yrs:g}+ years (limit {config.MAX_YEARS:g})")
    lakhs = _max_lakhs(job.get("salary"))
    if lakhs is not None and lakhs < config.MIN_SALARY_LPA:
        reasons.append(f"Pays at most ₹{lakhs:g}L (minimum ₹{config.MIN_SALARY_LPA:g}L)")
    places = [p.lower() for p in (job.get("locations") or []) + (job.get("hires_in") or [])]
    in_india = any(any(ip in p for ip in INDIA_PLACES) for p in places)
    remote_ok = job.get("remote") and (not job.get("hires_in") or any("india" in p.lower() for p in job["hires_in"]))
    visa_ok = str(job.get("visa") or "").lower().startswith("available")
    if not (in_india or remote_ok or visa_ok):
        reasons.append("Outside India without visa sponsorship")
    if job.get("apply_on_wellfound") is False:
        reasons.append("Applies on an external site")
    if job.get("eligibility_issues"):
        reasons.append(f"Wellfound eligibility: {job['eligibility_issues']}")
    return {"ok": not reasons, "reasons": reasons}


def answers_hash(answers: dict[str, str]) -> str:
    return hashlib.sha256(json.dumps(answers, sort_keys=True, ensure_ascii=False).encode()).hexdigest()


def _norm(text: str) -> str:
    return re.sub(r"\s+", " ", text.replace(",", "")).lower()


def check(answers: dict[str, str], questions: list[dict], profile_text: str, job: dict[str, Any]) -> dict:
    """Verify every checkable claim in the answers against the profile and posting."""
    job_text = " ".join(str(job.get(k) or "") for k in ("title", "company", "description", "salary"))
    job_text += " " + " ".join(job.get("locations") or []) + " " + " ".join(job.get("stack") or [])
    allowed = _norm(profile_text + " " + job_text + " " + " ".join(q["question"] for q in questions))
    profile_links = _norm(profile_text)
    # Links may point to the user's own pages (profile) or the employer's (posting).
    known_links = profile_links + " " + _norm(job_text)
    by_key = {q["key"]: q for q in questions}
    issues: list[str] = []

    for q in questions:
        if q["required"] and not answers.get(q["key"], "").strip():
            issues.append(f"Required question unanswered: {q['question']}")
    for key, text in answers.items():
        label = (by_key.get(key) or {}).get("question", key)[:50]
        if key not in by_key:
            issues.append(f"Answer for a question not on the form: {key}")
            continue
        if len(text) > MAX_ANSWER_CHARS:
            issues.append(f"'{label}': {len(text)} characters (max {MAX_ANSWER_CHARS})")
        if m := PLACEHOLDER.search(text):
            issues.append(f"'{label}': placeholder text {m.group()!r}")
        for n in NUMBER.findall(text):
            if _norm(n) not in allowed:
                issues.append(f"'{label}': number {n} isn't in the profile or posting")
        for e in EMAIL.findall(text):
            if e.lower() not in profile_links:
                issues.append(f"'{label}': email {e} isn't in the profile")
        for u in URL.findall(EMAIL.sub(" ", text)):
            bare = re.sub(r"^https?://(www\.)?", "", u.lower()).rstrip("/.")
            if bare not in known_links:
                issues.append(f"'{label}': link {u} isn't in the profile or posting")
        for w in WORD.findall(URL.sub(" ", EMAIL.sub(" ", text))):
            if not w[0].isupper():
                continue
            base = re.sub(r"(['’]s|['’])$", "", w).rstrip(".-/")
            if base.lower() in COMMON or len(base) < 2:
                continue
            if base.lower() not in allowed:
                issues.append(f"'{label}': '{base}' isn't in the profile or posting")
    unique = list(dict.fromkeys(issues))
    return {"passed": not unique, "issues": unique, "answers_hash": answers_hash(answers)}


def check_form(answers: dict[str, str], job_text: str, facts_text: str) -> dict:
    """check() for any company career-site form, not just Wellfound.

    answers maps each form question (its label, as shown) to the answer typed in.
    job_text is the posting (title, company, description); facts_text is
    profile.md plus personal_details.json. Every question counts as required.
    """
    questions = [{"key": q, "question": q, "required": True} for q in answers]
    return check(answers, questions, facts_text, {"description": job_text})
