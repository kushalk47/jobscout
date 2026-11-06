"""MCP server: small tools that return compact JSON.

Talks to Claude over stdio, so never print() to stdout here. Log to stderr.
"""

import logging
import sys
from contextlib import asynccontextmanager

from mcp.server.fastmcp import FastMCP

from . import config, profile, store, wellfound
from .browser import Browser, BrowserError

logging.basicConfig(stream=sys.stderr, level=logging.INFO)
browser = Browser()
# Title/company of jobs seen this session, so the log and previews can name them.
seen_jobs: dict[str, dict] = {}
# Form questions from draft_application, so the submit preview can show them.
drafts: dict[str, list[dict]] = {}


@asynccontextmanager
async def lifespan(_server: FastMCP):
    try:
        yield
    finally:
        await browser.close()


mcp = FastMCP("wellfound", lifespan=lifespan)


def _names(job_id: str) -> dict:
    j = seen_jobs.get(job_id) or store.get(job_id) or {}
    return {"title": j.get("title") or "", "company": j.get("company") or ""}


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


@mcp.tool()
async def get_job(job_id: str) -> dict:
    """Get one job's details: requirements, stack, compensation, trimmed description."""
    try:
        job = await wellfound.get_job(browser, job_id)
    except BrowserError as e:
        return {"error": str(e), "job_id": job_id}
    if "error" not in job:
        seen_jobs[job_id] = {**seen_jobs.get(job_id, {}), "title": job["title"], "company": job["company"]}
    return job


@mcp.tool()
def skip_job(job_ids: list[str], reason: str = "", confirm: bool = False, undo: bool = False) -> dict:
    """Mark jobs as not interesting so search hides them. With confirm=False (default)
    this only returns a preview: show it to the user and call again with confirm=True
    only if they approve. undo=True un-skips the given jobs instead."""
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
    Answer each question from the profile (get_profile); ask the user about anything it
    doesn't cover. Then call submit_application with answers keyed by each question's `key`."""
    if (seen := store.get(job_id)) and seen["status"] == "submitted":
        return {"error": "Already applied to this job.", "on": seen["updated_at"]}
    try:
        form = await wellfound.application_questions(browser, job_id)
    except (wellfound.ApplyError, BrowserError) as e:
        return {"error": str(e), "job_id": job_id}
    drafts[job_id] = form["questions"]
    store.record(job_id, "drafted", url=f"{config.BASE_URL}/jobs/{job_id}", **_names(job_id))
    return {"job_id": job_id, **_names(job_id), **form,
            "note": "Wellfound sends the user's Wellfound profile with the application; only these questions are filled here."}


@mcp.tool()
async def submit_application(job_id: str, answers: dict[str, str], confirm: bool = False) -> dict:
    """Submit an application. answers maps each question `key` from draft_application to
    the answer text. With confirm=False (default) this only returns a preview. Only call it
    with confirm=True after the user has explicitly approved this exact preview."""
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
                "next": "Show this to the user. Resubmit with confirm=True only if they approve."}
    if store.submitted_today() >= config.DAILY_APPLY_LIMIT:
        return {"error": f"Daily limit of {config.DAILY_APPLY_LIMIT} submissions reached."}
    try:
        await wellfound.submit(browser, job_id, answers)
    except (wellfound.ApplyError, BrowserError) as e:
        return {"submitted": False, "error": str(e), "job_id": job_id}
    store.record(job_id, "submitted", **_names(job_id))
    drafts.pop(job_id, None)
    return {"submitted": True, "job_id": job_id, **_names(job_id)}


@mcp.tool()
def list_applications(status: str | None = None, limit: int = 50) -> list[dict]:
    """List jobs from the local log. status: drafted | submitted | skipped.
    Use status="skipped" to let the user review what was skipped."""
    return store.list_all(status, limit)


def main() -> None:
    mcp.run()


if __name__ == "__main__":
    main()
