"""Wellfound page handling.

Everything site-specific lives here, so when Wellfound changes its markup
only this file needs fixing. The TODOs need working out against the live
site (run `uv run wellfound-mcp-login`, browse, and inspect the pages).
"""

from typing import Any
from urllib.parse import quote

from . import config
from .browser import Browser, next_data

NOT_READY = "Not implemented yet: Wellfound page parsing for this step still needs to be written."


async def is_logged_in(browser: Browser) -> bool:
    page = await browser.goto(f"{config.BASE_URL}/jobs")
    # TODO: verify. Assumes logged-out visitors get sent to a /login URL.
    return "/login" not in page.url


async def search_jobs(browser: Browser, role: str, location: str | None, remote: bool, limit: int) -> list[dict[str, Any]]:
    # TODO: confirm Wellfound's real search URL and filter parameters.
    url = f"{config.BASE_URL}/role/{quote(role.lower().replace(' ', '-'))}"
    if remote:
        url = f"{config.BASE_URL}/role/r/{quote(role.lower().replace(' ', '-'))}"
    elif location:
        url += f"/{quote(location.lower().replace(' ', '-'))}"
    page = await browser.goto(url)
    data = await next_data(page)
    # TODO: pull listings out of `data` (or the DOM) and return compact rows:
    #   {"id", "title", "company", "location", "remote", "salary", "summary"}
    # keeping `summary` to one line, then cut the list to `limit`.
    _ = (data, limit)
    raise NotImplementedError(NOT_READY)


async def get_job(browser: Browser, job_id: str) -> dict[str, Any]:
    page = await browser.goto(f"{config.BASE_URL}/jobs/{quote(job_id)}")
    data = await next_data(page)
    # TODO: return only what matters for deciding and applying:
    #   {"id", "title", "company", "url", "salary", "equity", "location",
    #    "remote", "requirements": [...], "stack": [...], "description"}
    # with `description` trimmed to ~1500 characters.
    _ = data
    raise NotImplementedError(NOT_READY)


async def application_questions(browser: Browser, job_id: str) -> list[dict[str, Any]]:
    # TODO: open the job's apply dialog and list each field as
    #   {"key", "label", "type": "text|textarea|select|checkbox", "required", "options"}
    raise NotImplementedError(NOT_READY)


async def submit(browser: Browser, job_id: str, answers: dict[str, str]) -> None:
    # TODO: open the apply dialog, fill each field by key, click submit,
    # and confirm the success state appears. Raise if it does not.
    raise NotImplementedError(NOT_READY)
