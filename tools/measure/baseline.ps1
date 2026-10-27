<#
.SYNOPSIS
  Aegis v3 - Phase 0 baseline network measurement (no VPN).

.DESCRIPTION
  Measures what the raw mobile link looks like BEFORE we build anything:
    - Round-trip time (min / avg / p95 / max) and jitter
    - Packet loss (overall and longest burst of consecutive losses)
    - Download and upload throughput
    - Which network / ISP / Cloudflare edge we are on

  How to use it: connect this laptop to your PHONE'S HOTSPOT (mobile data, Wi-Fi off on the phone),
  then run it once per scenario, e.g.:
    .\baseline.ps1 -Label "safaricom-4g-strong-indoor"
    .\baseline.ps1 -Label "safaricom-4g-weak-1bar"

  Results:
    docs\measurements\raw\<timestamp>_<label>.json   (full data, not committed)
    docs\measurements\results.csv                    (one summary row per run, committed)

  Limitation: ICMP ping can be de-prioritised by carriers. It is a good first look, not the
  final answer. Phase 0b repeats the test with UDP through our own relay.

  Works on Windows PowerShell 5.1 and PowerShell 7+.
#>
param(
    [Parameter(Mandatory = $true)][string]$Label,
    [int]$PingCount = 300,          # 300 pings x 200 ms = ~60 s per target
    [int]$PingIntervalMs = 200,
    [int]$PingTimeoutMs = 2000,
    [string[]]$Targets = @('1.1.1.1', '8.8.8.8', '9.9.9.9'),
    [int]$DownloadBytes = 25000000, # 25 MB
    [int]$UploadBytes = 5000000,    # 5 MB
    [switch]$SkipThroughput
)

$ErrorActionPreference = 'Stop'
$repoRoot = Resolve-Path (Join-Path $PSScriptRoot '..\..')
$rawDir = Join-Path $repoRoot 'docs\measurements\raw'
$csvPath = Join-Path $repoRoot 'docs\measurements\results.csv'
New-Item -ItemType Directory -Force -Path $rawDir | Out-Null

Add-Type -AssemblyName System.Net.Http

function Get-Percentile([double[]]$values, [double]$p) {
    if ($values.Count -eq 0) { return $null }
    $sorted = $values | Sort-Object
    $idx = [math]::Ceiling(($p / 100) * $sorted.Count) - 1
    if ($idx -lt 0) { $idx = 0 }
    return [math]::Round($sorted[$idx], 1)
}

function Measure-Ping([string]$target) {
    $pinger = New-Object System.Net.NetworkInformation.Ping
    $rtts = New-Object System.Collections.Generic.List[double]
    $lost = 0; $burst = 0; $maxBurst = 0
    for ($i = 1; $i -le $PingCount; $i++) {
        $t0 = [Diagnostics.Stopwatch]::StartNew()
        try {
            $reply = $pinger.Send($target, $PingTimeoutMs)
            $ok = ($reply.Status -eq 'Success')
        } catch { $ok = $false }
        if ($ok) {
            $rtts.Add([double]$reply.RoundtripTime); $burst = 0
        } else {
            $lost++; $burst++; if ($burst -gt $maxBurst) { $maxBurst = $burst }
        }
        if ($i % 50 -eq 0) { Write-Host ("    {0}: {1}/{2} sent, {3} lost" -f $target, $i, $PingCount, $lost) }
        $wait = $PingIntervalMs - $t0.ElapsedMilliseconds
        if ($wait -gt 0) { Start-Sleep -Milliseconds $wait }
    }
    $arr = [double[]]$rtts.ToArray()
    $jitter = $null
    if ($arr.Count -gt 1) {
        $diffs = for ($j = 1; $j -lt $arr.Count; $j++) { [math]::Abs($arr[$j] - $arr[$j - 1]) }
        $jitter = [math]::Round(($diffs | Measure-Object -Average).Average, 1)
    }
    $stats = $null
    if ($arr.Count -gt 0) { $stats = $arr | Measure-Object -Minimum -Maximum -Average }
    [pscustomobject]@{
        target         = $target
        sent           = $PingCount
        lost           = $lost
        lossPct        = [math]::Round(100.0 * $lost / $PingCount, 2)
        maxLossBurst   = $maxBurst
        rttMinMs       = if ($stats) { $stats.Minimum } else { $null }
        rttAvgMs       = if ($stats) { [math]::Round($stats.Average, 1) } else { $null }
        rttP95Ms       = Get-Percentile $arr 95
        rttMaxMs       = if ($stats) { $stats.Maximum } else { $null }
        jitterMs       = $jitter
    }
}

function Measure-Download([int]$bytes) {
    $client = New-Object System.Net.Http.HttpClient
    $client.Timeout = [TimeSpan]::FromSeconds(120)
    $sw = [Diagnostics.Stopwatch]::StartNew()
    $resp = $client.GetAsync("https://speed.cloudflare.com/__down?bytes=$bytes", [System.Net.Http.HttpCompletionOption]::ResponseHeadersRead).Result
    $stream = $resp.Content.ReadAsStreamAsync().Result
    $buf = New-Object byte[] 65536; $total = 0
    while (($n = $stream.Read($buf, 0, $buf.Length)) -gt 0) { $total += $n }
    $sw.Stop(); $client.Dispose()
    [math]::Round(($total * 8) / $sw.Elapsed.TotalSeconds / 1e6, 2)
}

function Measure-Upload([int]$bytes) {
    $client = New-Object System.Net.Http.HttpClient
    $client.Timeout = [TimeSpan]::FromSeconds(120)
    $payload = New-Object byte[] $bytes
    (New-Object Random).NextBytes($payload)
    $content = New-Object System.Net.Http.ByteArrayContent(, $payload)
    $sw = [Diagnostics.Stopwatch]::StartNew()
    $null = $client.PostAsync('https://speed.cloudflare.com/__up', $content).Result
    $sw.Stop(); $client.Dispose()
    [math]::Round(($bytes * 8) / $sw.Elapsed.TotalSeconds / 1e6, 2)
}

function Get-NetworkInfo {
    $info = [ordered]@{}
    try {
        $trace = (Invoke-WebRequest -Uri 'https://1.1.1.1/cdn-cgi/trace' -UseBasicParsing -TimeoutSec 10).Content
        foreach ($line in ($trace -split "`n")) {
            $kv = $line -split '=', 2
            if ($kv.Count -eq 2 -and @('ip', 'loc', 'colo', 'warp') -contains $kv[0]) { $info[$kv[0]] = $kv[1].Trim() }
        }
    } catch { $info['traceError'] = $_.Exception.Message }
    try {
        $wlan = (netsh wlan show interfaces) -join "`n"
        if ($wlan -match '(?m)^\s*SSID\s*:\s*(.+)$') { $info['ssid'] = $Matches[1].Trim() }
        if ($wlan -match '(?m)^\s*Signal\s*:\s*(.+)$') { $info['wifiSignal'] = $Matches[1].Trim() }
    } catch {}
    [pscustomobject]$info
}

# ---------------- run ----------------
$stamp = Get-Date -Format 'yyyyMMdd-HHmmss'
Write-Host "== Aegis Phase 0 baseline: '$Label' ($stamp) ==" -ForegroundColor Cyan

Write-Host '[1/3] Network info...' -ForegroundColor Yellow
$net = Get-NetworkInfo
$net | Format-List | Out-String | Write-Host

Write-Host "[2/3] Latency & loss ($PingCount pings per target, every $PingIntervalMs ms)..." -ForegroundColor Yellow
$pingResults = foreach ($t in $Targets) { Measure-Ping $t }
$pingResults | Format-Table -AutoSize | Out-String | Write-Host

$down = $null; $up = $null
if (-not $SkipThroughput) {
    Write-Host '[3/3] Throughput (Cloudflare speed endpoints)...' -ForegroundColor Yellow
    try { $down = Measure-Download $DownloadBytes; Write-Host "    Download: $down Mbps" } catch { Write-Host "    Download failed: $($_.Exception.Message)" -ForegroundColor Red }
    try { $up = Measure-Upload $UploadBytes; Write-Host "    Upload:   $up Mbps" } catch { Write-Host "    Upload failed: $($_.Exception.Message)" -ForegroundColor Red }
}

$result = [ordered]@{
    label      = $Label
    timestamp  = $stamp
    network    = $net
    ping       = $pingResults
    downMbps   = $down
    upMbps     = $up
    settings   = @{ pingCount = $PingCount; intervalMs = $PingIntervalMs; timeoutMs = $PingTimeoutMs }
}
$rawFile = Join-Path $rawDir "${stamp}_$($Label -replace '[^\w\-]', '_').json"
$result | ConvertTo-Json -Depth 6 | Set-Content -Path $rawFile -Encoding utf8

# One summary row: worst-case loss across targets, average RTT across targets
$valid = $pingResults | Where-Object { $_.rttAvgMs -ne $null }
$row = [pscustomobject]@{
    timestamp     = $stamp
    label         = $Label
    publicIp      = $net.ip
    cfColo        = $net.colo
    worstLossPct  = ($pingResults | Measure-Object lossPct -Maximum).Maximum
    worstBurst    = ($pingResults | Measure-Object maxLossBurst -Maximum).Maximum
    avgRttMs      = if ($valid) { [math]::Round(($valid | Measure-Object rttAvgMs -Average).Average, 1) } else { $null }
    worstP95Ms    = if ($valid) { ($valid | Measure-Object rttP95Ms -Maximum).Maximum } else { $null }
    avgJitterMs   = if ($valid) { [math]::Round(($valid | Measure-Object jitterMs -Average).Average, 1) } else { $null }
    downMbps      = $down
    upMbps        = $up
}
$row | Export-Csv -Path $csvPath -Append -NoTypeInformation -Encoding utf8

Write-Host ''
Write-Host '== Summary ==' -ForegroundColor Green
$row | Format-List | Out-String | Write-Host
Write-Host "Raw data: $rawFile"
Write-Host "Summary appended to: $csvPath"
