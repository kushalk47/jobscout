"""Settings, read from environment variables."""

import os
from pathlib import Path

from platformdirs import user_data_dir

BASE_URL = "https://wellfound.com"

# ~/.local/share/wellfound-mcp by default. Holds your logged-in browser
# profile (treat it like a password) and the local application log.
DATA_DIR = Path(os.environ.get("WELLFOUND_MCP_DATA", user_data_dir("wellfound-mcp")))
PROFILE_DIR = DATA_DIR / "browser-profile"
DB_PATH = DATA_DIR / "applications.db"

# Visible browser by default: bot checks are less likely to trip, and you
# can watch what it does. Set WELLFOUND_HEADLESS=1 to hide it.
HEADLESS = os.environ.get("WELLFOUND_HEADLESS", "0") == "1"

# Seconds to wait between page loads, picked at random in this range.
MIN_DELAY = float(os.environ.get("WELLFOUND_MIN_DELAY", "3"))
MAX_DELAY = float(os.environ.get("WELLFOUND_MAX_DELAY", "7"))

# Hard cap on submissions per day, enforced from the local log.
DAILY_APPLY_LIMIT = int(os.environ.get("WELLFOUND_DAILY_LIMIT", "15"))

# Your background and standard answers, used to fill application forms.
# Lives outside the repo by default because it holds personal details.
PROFILE_PATH = Path(os.environ.get("WELLFOUND_PROFILE", DATA_DIR / "profile.md"))
