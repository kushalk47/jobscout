"""MCP server: small tools that return compact JSON.

Talks to Claude over stdio, so never print() to stdout here. Log to stderr.
"""

import asyncio
import functools
import inspect
import json
import logging
import sys
from contextlib import asynccontextmanager

from mcp.server.fastmcp import FastMCP

from . import config, factcheck, notify, profile, store, wellfound
from .browser import Browser, BrowserError

logging.basicConfig(stream=sys.stderr, level=logging.INFO)
browser = Browser()
# Title/company of jobs seen this session, so the log and previews can name them.
seen_jobs: dict[str, dict] = {}
# Form questions from draft_application, so the submit preview can show them.
drafts: dict[str, list[dict]] = {}
# Full job details (incl. description) for fit checks and fact checks.
job_cache: dict[str, dict] = {}
# job_id -> hash of the exact answers that passed fact_check.
checked: dict[str, str] = {}


@asynccontextmanager
async def lifespan(_server: FastMCP):
    try:
        yield
    finally:
        await browser.close()


# Workflow guidance lives here once, instead of being repeated in every tool description.
INSTRUCTIONS = (
    "Wellfound job applications. Flow: get_profile once; search_jobs; get_job (check fit.ok, "
    "skip_job the misfits with the fit reasons); draft_application (questions + description); "
    "write answers ONLY from the profile; fact_check; then submit_application, or "
    "hold_application if anything fails or you're unsure. Never invent facts. "
    + ("AUTO MODE ON: the user gave standing permission to submit and skip without asking, but only "
       "jobs with fit.ok and answers that passed fact_check after your own sentence-by-sentence review."
       if config.AUTO_APPLY else
       "Manual mode: show the submit preview and send with confirm=true only after the user approves it; "
       "skip_job also needs the user's OK.")
)
mcp = FastMCP("wellfound", instructions=INSTRUCTIONS, lifespan=lifespan)


def _lean(x):
    """Drop None and empty strings so results stay small."""
    if isinstance(x, dict):
        return {k: _lean(v) for k, v in x.items() if v is not None and v != ""}
    if isinstance(x, list):
        return [_lean(v) for v in x]
    return x


def tool(description: str | None = None):
    """Register a tool whose result goes back as ONE compact JSON text block.

    By default FastMCP indents JSON, splits lists into one block per item, and
    for some return types sends the data a second time as structured content.
    """
    def deco(fn):
        @functools.wraps(fn)
        async def wrapper(*args, **kwargs):
            result = fn(*args, **kwargs)
            if inspect.isawaitable(result):
                result = await result
            return json.dumps(_lean(result), ensure_ascii=False, separators=(",", ":"))
        wrapper.__signature__ = inspect.signature(fn).replace(return_annotation=str)
        wrapper.__annotations__ = {**fn.__annotations__, "return": str}
        return mcp.tool(description=description or inspect.getdoc(fn), structured_output=False)(wrapper)
    return deco


AUTO_SUBMIT_DOC = "Send an application (answers keyed by question key). Auto mode: confirm=true only if fit.ok and fact_check passed on these exact answers; the server enforces it. confirm=false previews."
MANUAL_SUBMIT_DOC = "Send an application (answers keyed by question key). confirm=false previews; confirm=true only after the user approves that exact preview."


def _names(job_id: str) -> dict:
    j = seen_jobs.get(job_id) or store.get(job_id) or {}
    return {"title": j.get("title") or "", "company": j.get("company") or ""}


def _apply_error(e: Exception, job_id: str) -> dict:
    out = {"error": str(e), "job_id": job_id, "wellfound_says": getattr(e, "notice", None)}
    if getattr(e, "limit_reached", False):
        store.record_limit_hit()
        out["limit_reached"] = True
        out["next"] = "Stop applying for now and tell the user; slots free up as older applications expire."
    return {k: v for k, v in out.items() if v is not None}


@tool()
async def check_login() -> dict:
    """Is the saved Wellfound session logged in?"""
    try:
        ok = await wellfound.is_logged_in(browser)
    except BrowserError as e:
        return {"logged_in": None, "error": str(e)}
    return {"logged_in": ok, "hint": None if ok else "Run `uv run wellfound-mcp-login` and sign in."}


@tool()
def get_profile() -> dict:
    """The user's profile.md. Read once per session; answer only from it."""
    return profile.read()


@tool()
def save_answer(question: str, answer: str) -> dict:
    """Append an answer the user gave to profile.md. Only with their OK."""
    return profile.save_answer(question, answer)


@tool()
async def search_jobs(role: str, location: str | None = None, remote: bool = False, limit: int = 10,
                      summaries: bool = False) -> dict:
    """Search jobs. Hides skipped ones; marks drafted/held/submitted. summaries=true adds a one-line summary per job."""
    try:
        rows = await wellfound.search_jobs(browser, role, location, remote, min(limit, 25))
    except BrowserError as e:
        return {"error": str(e)}
    out, hidden = [], 0
    for r in rows:
        seen_jobs[r["id"]] = r
        if not summaries:
            r = {k: v for k, v in r.items() if k != "summary"}
        if seen := store.get(r["id"]):
            if seen["status"] == "skipped":
                hidden += 1
                continue
            r["already"] = seen["status"]
        out.append(r)
    return {"jobs": out, "hidden_skipped": hidden}


def _remember(job_id: str, job: dict) -> None:
    job_cache[job_id] = job
    seen_jobs[job_id] = {**seen_jobs.get(job_id, {}), "title": job["title"], "company": job["company"],
                         "salary": job.get("salary"), "location": ", ".join(job.get("locations") or [])}


async def _wait_for_scheduled() -> dict | None:
    """Hold off while an earlier application is still in its undo window.
    Returns None when it's fine to send, or an error dict asking to retry later."""
    if not config.WAIT_FOR_SCHEDULED:
        return None
    waited = 0.0
    while True:
        left = await wellfound.scheduled_remaining(browser)
        if left <= config.SCHEDULED_SLACK_S:
            return None
        if waited >= config.SCHEDULED_MAX_WAIT_S:
            return {"submitted": False, "waiting": True, "retry_after_seconds": left,
                    "error": f"The previous application is still scheduled ({left}s left). Call again after that."}
        pause = min(left - config.SCHEDULED_SLACK_S + 2, 60, config.SCHEDULED_MAX_WAIT_S - waited)
        logging.info("Previous application still scheduled (%ss left); waiting %.0fs", left, pause)
        await asyncio.sleep(max(pause, 5))
        waited += max(pause, 5)


def _job_for_email(job_id: str) -> dict:
    return {**seen_jobs.get(job_id, {}), **_names(job_id), "id": job_id, "url": f"{config.BASE_URL}/jobs/{job_id}"}


@tool()
async def get_job(job_id: str, full: bool = False) -> dict:
    """Job details and fit (rules: <=3 yrs, pay floor, India or visa, applies on Wellfound). full=true adds the description."""
    try:
        job = await wellfound.get_job(browser, job_id)
    except BrowserError as e:
        return {"error": str(e), "job_id": job_id}
    if "error" not in job:
        _remember(job_id, job)
        job["fit"] = factcheck.fit(job)          # uses the description, so check before trimming
        if not full:
            job = {k: v for k, v in job.items() if k != "description"}
    return job


@tool(description="Skip jobs so search hides them (reason recommended). confirm=false previews. undo=true un-skips.")
def skip_job(job_ids: list[str], reason: str = "", confirm: bool = False, undo: bool = False) -> dict:
    if undo:
        if not confirm:
            return {"preview": True, "would_unskip": [{"job_id": j, **_names(j)} for j in job_ids]}
        done = [j for j in job_ids if (s := store.get(j)) and s["status"] == "skipped"]
        for j in done:
            store.delete(j)
        return {"unskipped": done}

    applied = [j for j in job_ids if (s := store.get(j)) and s["status"] == "submitted"]
    todo = [j for j in job_ids if j not in applied]
    if not confirm:
        return {"preview": True, "reason": reason,
                "would_skip": [{"job_id": j, **_names(j)} for j in todo],
                "not_skippable_already_applied": applied or None,
                "next": "Show this list to the user. Call again with confirm=True only if they approve."}
    for j in todo:
        store.record(j, "skipped", notes=reason, **_names(j))
    return {"skipped": todo, "not_skippable_already_applied": applied or None}


@tool()
async def draft_application(job_id: str) -> dict:
    """Open the apply form: questions (answer by key), warnings, fit and the job description. Sends nothing."""
    if (seen := store.get(job_id)) and seen["status"] == "submitted":
        return {"error": "Already applied to this job.", "on": seen["updated_at"]}
    try:
        form = await wellfound.application_questions(browser, job_id)
    except (wellfound.ApplyError, BrowserError) as e:
        return _apply_error(e, job_id)
    job = form.pop("job", None)
    if job:
        _remember(job_id, job)
    drafts[job_id] = form["questions"]
    checked.pop(job_id, None)
    store.record(job_id, "drafted", url=f"{config.BASE_URL}/jobs/{job_id}", **_names(job_id))
    return {"job_id": job_id, **_names(job_id), **form,
            "fit": factcheck.fit(job) if job else None,
            "description": (job or {}).get("description", "")[:1000] or None,
            "note": "Wellfound sends the user's Wellfound profile with the application; only these questions are filled here."}


@tool()
def fact_check(job_id: str, answers: dict[str, str]) -> dict:
    """Check answers: every number, capitalised name, link and email must be in the profile or posting. Can't catch true facts paired wrongly; that's your review."""
    questions = drafts.get(job_id)
    if questions is None:
        return {"error": "Run draft_application for this job first."}
    prof = profile.read()
    if not prof.get("profile"):
        return {"error": "No profile.md to check against.", **prof}
    result = factcheck.check(answers, questions, prof["profile"], job_cache.get(job_id, {}))
    if result["passed"]:
        checked[job_id] = result["answers_hash"]
    else:
        checked.pop(job_id, None)
    return {"passed": result["passed"], "issues": result["issues"] or None}


@tool()
async def hold_application(job_id: str, answers: dict[str, str], reason: str) -> dict:
    """Don't send; log it as held and email the user the draft and reason."""
    questions = drafts.get(job_id, [])
    rows = [{"question": q["question"], "answer": answers.get(q["key"], "")} for q in questions]
    store.record(job_id, "held", notes=reason, url=f"{config.BASE_URL}/jobs/{job_id}", **_names(job_id))
    email = await asyncio.to_thread(notify.application_held, _job_for_email(job_id), rows, reason)
    return {"held": True, "job_id": job_id, **_names(job_id), "email_alert": email}


@tool(description=AUTO_SUBMIT_DOC if config.AUTO_APPLY else MANUAL_SUBMIT_DOC)
async def submit_application(job_id: str, answers: dict[str, str], confirm: bool = False) -> dict:
    if (seen := store.get(job_id)) and seen["status"] == "submitted":
        return {"error": "Already applied to this job.", "on": seen["updated_at"]}
    questions = drafts.get(job_id)
    if not confirm:
        if questions is None:
            return {"error": "Run draft_application for this job first, so the preview shows the real questions."}
        rows = [{"question": q["question"], "required": q["required"], "answer": answers.get(q["key"], "")}
                for q in questions]
        missing = [r["question"] for r in rows if r["required"] and not r["answer"].strip()]
        unknown = sorted(set(answers) - {q["key"] for q in questions})
        return {"preview": True, "job_id": job_id, **_names(job_id), "answers": rows,
                "missing_required": missing or None, "unknown_keys": unknown or None,
                "fact_checked": checked.get(job_id) == factcheck.answers_hash(answers),
                "today": store.daily_limit(),
                "next": ("Auto mode: send only if fact_checked and fit.ok and you're confident; otherwise hold_application."
                         if config.AUTO_APPLY else
                         "Show this to the user. Resubmit with confirm=True only if they approve.")}
    if config.AUTO_APPLY:
        # The server enforces the auto-mode rules; Claude's judgement is the second check.
        if checked.get(job_id) != factcheck.answers_hash(answers):
            return {"submitted": False, "error": "Auto mode: these exact answers haven't passed fact_check. "
                    "Run fact_check, or hold_application if it can't pass."}
        if not (fit := factcheck.fit(job_cache.get(job_id, {})))["ok"]:
            return {"submitted": False, "error": f"Auto mode: job doesn't fit the rules: {fit['reasons']}. Skip it."}
    today = store.daily_limit()
    if today["submitted_today"] >= today["limit_today"]:
        return {"error": f"Today's limit of {today['limit_today']} applications is reached. "
                         "It rises by 1 each day you apply without Wellfound blocking you.", **today}
    try:
        if busy := await _wait_for_scheduled():
            return {**busy, "job_id": job_id}
        await wellfound.submit(browser, job_id, answers)
    except (wellfound.ApplyError, BrowserError) as e:
        return {"submitted": False, **_apply_error(e, job_id)}
    store.record(job_id, "submitted", **_names(job_id))
    checked.pop(job_id, None)
    sent = [{"question": q["question"], "answer": answers.get(q["key"], "")} for q in drafts.pop(job_id, [])]
    email = await asyncio.to_thread(notify.application_sent, _job_for_email(job_id), sent)
    return {"submitted": True, "job_id": job_id, **_names(job_id), "email_alert": email}


@tool()
def list_applications(status: str | None = None, limit: int = 50) -> list[dict]:
    """Local log. status: submitted | held | drafted | skipped."""
    return store.list_all(status, limit)


def main() -> None:
    mcp.run()


if __name__ == "__main__":
    main()
