"""Settings, read from environment variables."""

import os
from pathlib import Path

BASE_URL = "https://wellfound.com"

# Everything lives inside the project folder, and private files are gitignored.
PROJECT_ROOT = Path(__file__).resolve().parents[2]

# ./data by default. Holds your logged-in browser profile (treat it like a
# password) and the local application log.
DATA_DIR = Path(os.environ.get("WELLFOUND_MCP_DATA", PROJECT_ROOT / "data"))
PROFILE_DIR = DATA_DIR / "browser-profile"
DB_PATH = DATA_DIR / "applications.db"

# Visible browser by default: bot checks are less likely to trip, and you
# can watch what it does. Set WELLFOUND_HEADLESS=1 to hide it.
HEADLESS = os.environ.get("WELLFOUND_HEADLESS", "0") == "1"

# Seconds to wait between page loads, picked at random in this range.
MIN_DELAY = float(os.environ.get("WELLFOUND_MIN_DELAY", "3"))
MAX_DELAY = float(os.environ.get("WELLFOUND_MAX_DELAY", "7"))

# Hard cap on submissions per day, enforced from the local log.
# The daily cap ramps up: it starts at DAILY_START, rises by 1 for every past day
# you applied without hitting Wellfound's limit on active applications, and stops
# at DAILY_MAX. Hitting Wellfound's limit drops it back to DAILY_START.
DAILY_START = int(os.environ.get("WELLFOUND_DAILY_START", "5"))
DAILY_MAX = int(os.environ.get("WELLFOUND_DAILY_LIMIT", "15"))

# Your background and standard answers, used to fill application forms.
# Copy profile.example.md to profile.md and fill it in. Gitignored, because it
# holds personal details.
PROFILE_PATH = Path(os.environ.get("WELLFOUND_PROFILE", PROJECT_ROOT / "profile.md"))
