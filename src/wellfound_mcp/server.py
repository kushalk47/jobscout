"""MCP server: small tools that return compact JSON.

Talks to Claude over stdio, so never print() to stdout here. Log to stderr.
"""

import logging
import sys
from contextlib import asynccontextmanager

import asyncio

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


mcp = FastMCP("wellfound", lifespan=lifespan)

AUTO_SUBMIT_DOC = "Submit an application. answers maps each question `key` from draft_application to\nthe answer text. AUTO MODE IS ON: the user has given standing permission to send without asking,\nbut ONLY when (1) get_job's `fit.ok` is true, (2) you reviewed every sentence against the profile\nand are confident it is accurate and well written, and (3) fact_check passed on these exact answers.\nThe server refuses confirm=True otherwise. If you are unsure about anything, call hold_application\ninstead. confirm=False returns a preview."
MANUAL_SUBMIT_DOC = 'Submit an application. answers maps each question `key` from draft_application to\nthe answer text. With confirm=False (default) this only returns a preview. Only call it with\nconfirm=True after the user has explicitly approved this exact preview.'


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


@mcp.tool()
async def check_login() -> dict:
    """Check whether the saved browser session is logged in to Wellfound."""
    try:
        ok = await wellfound.is_logged_in(browser)
    except BrowserError as e:
        return {"logged_in": None, "error": str(e)}
    return {"logged_in": ok, "hint": None if ok else "Run `uv run wellfound-mcp-login` and sign in."}


@mcp.tool()
def get_profile() -> dict:
    """Read the user's profile.md (background and standard answers). Call once per
    session before drafting answers. If a form asks something the profile doesn't
    cover, ask the user; never invent an answer."""
    return profile.read()


@mcp.tool()
def save_answer(question: str, answer: str) -> dict:
    """Append an answer the user gave to profile.md, so the question is covered next
    time. Only call after the user agrees to save it."""
    return profile.save_answer(question, answer)


@mcp.tool()
async def search_jobs(role: str, location: str | None = None, remote: bool = False, limit: int = 10) -> dict:
    """Search Wellfound jobs. Returns compact rows (id, title, company, salary, one-line summary).
    Jobs the user skipped are hidden; jobs already drafted or applied to are marked."""
    try:
        rows = await wellfound.search_jobs(browser, role, location, remote, min(limit, 25))
    except BrowserError as e:
        return {"error": str(e)}
    out, hidden = [], 0
    for r in rows:
        seen_jobs[r["id"]] = r
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


def _job_for_email(job_id: str) -> dict:
    return {**seen_jobs.get(job_id, {}), **_names(job_id), "id": job_id, "url": f"{config.BASE_URL}/jobs/{job_id}"}


@mcp.tool()
async def get_job(job_id: str) -> dict:
    """Get one job's details: requirements, stack, compensation, trimmed description, and
    `fit`: whether it meets the user's rules (<= max years, pay floor, India or visa, applied on
    Wellfound). Never apply when fit.ok is false; skip it with the reasons instead."""
    try:
        job = await wellfound.get_job(browser, job_id)
    except BrowserError as e:
        return {"error": str(e), "job_id": job_id}
    if "error" not in job:
        _remember(job_id, job)
        job["fit"] = factcheck.fit(job)
    return job


@mcp.tool(description=(
    "Mark jobs as not interesting so search hides them. "
    + ("Auto mode is on: you may call confirm=True directly for jobs whose fit.ok is false, "
       "using the fit reasons as `reason`. " if config.AUTO_APPLY else
       "With confirm=False (default) this only returns a preview: show it to the user and call "
       "again with confirm=True only if they approve. ")
    + "undo=True un-skips the given jobs instead."))
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


@mcp.tool()
async def draft_application(job_id: str) -> dict:
    """Open a job's application form and list its questions. Nothing is submitted.
    Answer each question only from the profile (get_profile). Then run fact_check on the
    answers before submit_application. Answers are keyed by each question's `key`."""
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
            "note": "Wellfound sends the user's Wellfound profile with the application; only these questions are filled here."}


@mcp.tool()
def fact_check(job_id: str, answers: dict[str, str]) -> dict:
    """Check drafted answers against profile.md and the job posting: every number, capitalised
    name (employer, project, tool, place), link and email must appear there; no placeholders;
    required questions answered; length limits. It can't tell whether true facts are combined
    correctly (e.g. a project credited to the wrong employer); that part is your own review.
    If it fails, fix the answer from the profile and re-check, or hold_application."""
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


@mcp.tool()
async def hold_application(job_id: str, answers: dict[str, str], reason: str) -> dict:
    """Don't send; keep the draft for the user and email them an alert with the draft and
    `reason`. Use whenever fact_check fails and you can't fix it from the profile, the form
    asks something the profile doesn't cover, or you're not confident the answer is right."""
    questions = drafts.get(job_id, [])
    rows = [{"question": q["question"], "answer": answers.get(q["key"], "")} for q in questions]
    store.record(job_id, "held", notes=reason, url=f"{config.BASE_URL}/jobs/{job_id}", **_names(job_id))
    email = await asyncio.to_thread(notify.application_held, _job_for_email(job_id), rows, reason)
    return {"held": True, "job_id": job_id, **_names(job_id), "email_alert": email}


@mcp.tool(description=AUTO_SUBMIT_DOC if config.AUTO_APPLY else MANUAL_SUBMIT_DOC)
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
        await wellfound.submit(browser, job_id, answers)
    except (wellfound.ApplyError, BrowserError) as e:
        return {"submitted": False, **_apply_error(e, job_id)}
    store.record(job_id, "submitted", **_names(job_id))
    checked.pop(job_id, None)
    sent = [{"question": q["question"], "answer": answers.get(q["key"], "")} for q in drafts.pop(job_id, [])]
    email = await asyncio.to_thread(notify.application_sent, _job_for_email(job_id), sent)
    return {"submitted": True, "job_id": job_id, **_names(job_id), "email_alert": email}


@mcp.tool()
def list_applications(status: str | None = None, limit: int = 50) -> list[dict]:
    """List jobs from the local log. status: drafted | submitted | skipped | held.
    Use status="skipped" or "held" to let the user review those."""
    return store.list_all(status, limit)


def main() -> None:
    mcp.run()


if __name__ == "__main__":
    main()
