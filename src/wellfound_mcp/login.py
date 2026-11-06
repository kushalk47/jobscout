"""Open a visible browser on the saved profile so you can log in by hand.

Run with: uv run wellfound-mcp-login
Close Claude Code (or the MCP server) first: Chromium can't open the same
profile twice.
"""

import asyncio

from playwright.async_api import async_playwright

from . import config


async def _run() -> None:
    config.PROFILE_DIR.mkdir(parents=True, exist_ok=True)
    async with async_playwright() as pw:
        ctx = await pw.chromium.launch_persistent_context(str(config.PROFILE_DIR), headless=False)
        page = ctx.pages[0] if ctx.pages else await ctx.new_page()
        await page.goto(f"{config.BASE_URL}/login")
        print(f"Profile: {config.PROFILE_DIR}")
        print("Log in to Wellfound in the browser window, then press Enter here.")
        await asyncio.to_thread(input)
        await ctx.close()
    print("Saved. The MCP server will reuse this session.")


def main() -> None:
    asyncio.run(_run())


if __name__ == "__main__":
    main()
