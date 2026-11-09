# Start Claude Code on today's batch, at most once per day (Windows twin of daily-apply.sh).
#   powershell -File scripts\daily-apply.ps1          run if it hasn't run today
#   powershell -File scripts\daily-apply.ps1 -Force   run anyway
# To run it at logon: Task Scheduler > Create Task > Trigger "At log on" >
#   Action: powershell.exe -NoProfile -File "<repo>\hireloop\scripts\daily-apply.ps1"
param([switch]$Force)
$ErrorActionPreference = "Stop"
Set-Location (Join-Path $PSScriptRoot "..")

$home_ = if ($env:JOBSCOUT_HOME) { $env:JOBSCOUT_HOME } else { Join-Path $HOME ".jobscout" }
$dataDir = if (Test-Path "data") { "data" } else { Join-Path $home_ "wellfound" }
New-Item -ItemType Directory -Force $dataDir | Out-Null
$marker = Join-Path $dataDir "last-daily-run"
$today = Get-Date -Format "yyyy-MM-dd"
if (-not $Force -and (Test-Path $marker) -and ((Get-Content $marker -Raw).Trim() -eq $today)) {
    Write-Host "Today's batch already ran ($today). Use -Force to run again."
    exit 0
}
Set-Content -Path $marker -Value $today -Encoding utf8

# Give the network and the desktop a moment after logon.
$delay = if ($env:WELLFOUND_DAILY_DELAY) { [int]$env:WELLFOUND_DAILY_DELAY } else { 20 }
Start-Sleep -Seconds $delay
claude (Get-Content "scripts/daily-prompt.md" -Raw)
