"""One shared, persistent Chromium session, opened lazily."""

import asyncio
import json
import random
import time
from typing import Any

from playwright.async_api import BrowserContext, Page, Playwright, async_playwright

from . import config


class Browser:
    def __init__(self) -> None:
        self._pw: Playwright | None = None
        self._ctx: BrowserContext | None = None
        self._lock = asyncio.Lock()
        self._last_nav = 0.0

    async def page(self) -> Page:
        async with self._lock:
            if self._ctx is None:
                config.PROFILE_DIR.mkdir(parents=True, exist_ok=True)
                self._pw = await async_playwright().start()
                self._ctx = await self._pw.chromium.launch_persistent_context(
                    str(config.PROFILE_DIR),
                    headless=config.HEADLESS,
                    viewport={"width": 1280, "height": 900},
                )
            pages = self._ctx.pages
            return pages[0] if pages else await self._ctx.new_page()

    async def goto(self, url: str) -> Page:
        """Navigate at human speed: wait a random delay since the last load."""
        page = await self.page()
        wait = random.uniform(config.MIN_DELAY, config.MAX_DELAY) - (time.monotonic() - self._last_nav)
        if wait > 0:
            await asyncio.sleep(wait)
        await page.goto(url, wait_until="domcontentloaded")
        self._last_nav = time.monotonic()
        return page

    async def close(self) -> None:
        if self._ctx:
            await self._ctx.close()
        if self._pw:
            await self._pw.stop()
        self._ctx = self._pw = None


async def next_data(page: Page) -> dict[str, Any] | None:
    """Return the page's embedded Next.js JSON, if it has any.

    Wellfound appears to be a Next.js app. If its pages embed data in
    <script id="__NEXT_DATA__">, parsing that is far sturdier than CSS
    selectors. Verify against the live site before relying on it.
    """
    raw = await page.evaluate(
        "() => document.getElementById('__NEXT_DATA__')?.textContent ?? null"
    )
    return json.loads(raw) if raw else None
