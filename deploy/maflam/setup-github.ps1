<#
.SYNOPSIS
  Creates Maflam Trading's GitHub environments (maflam-testing, maflam-live) and fills
  their variables and secrets from .env.maflam-testing / .env.maflam-live, using gh.

.DESCRIPTION
  Run from anywhere, signed in with `gh auth login` as a repository admin:

    pwsh deploy/maflam/setup-github.ps1                  # both environments
    pwsh deploy/maflam/setup-github.ps1 -Targets testing # one
    pwsh deploy/maflam/setup-github.ps1 -DryRun          # show what would be set (names only)

  The values come from two files next to docker-compose.yml, which git ignores (.env.*):
    .env.maflam-testing   .env.maflam-live
  A missing file is created from deploy/maflam/maflam.env.example; fill it in and run again.

  DB_PASSWORD, REDIS_PASSWORD, JWT_SECRET and OTP_SECRET are generated when empty and
  written back into the file, so running the script again keeps them (a changed
  DB_PASSWORD would lock the server out of its existing database).

  maflam-live is limited to the main branch. With -RequireApproval the script also makes you
  (or -LiveReviewer) a required approver for live; without it, any approver is removed (live
  deploys are still manual-only). GitHub only allows approvers on private repositories with
  GitHub Enterprise, so on other plans it says so and carries on.
#>
[CmdletBinding()]
param(
  [string]   $Repo = "Total-Retail/stocktake-server",
  [ValidateSet("testing", "live")]
  [string[]] $Targets = @("testing", "live"),
  [string]   $LiveReviewer = "",
  [switch]   $RequireApproval,
  [switch]   $DryRun
)

$ErrorActionPreference = "Stop"
$serverRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path
$template   = Join-Path $PSScriptRoot "maflam.env.example"

# What goes where. Anything not listed here is ignored.
$secretNames = @("DB_PASSWORD", "REDIS_PASSWORD", "JWT_SECRET", "OTP_SECRET", "VANTAGE_CLIENT_SECRET", "SMS_API_KEY")
$varNames    = @("SSH_HOST", "SSH_PORT", "SSH_USER", "SSH_KNOWN_HOSTS", "APP_DIR", "HTTP_PORT", "PROXY_NETWORK", "PROXY_LETSENCRYPT", "PUBLIC_URL", "PUBLIC_WS_URL",
                 "DEPLOYMENT_NAME", "VANTAGE_TENANT_ID", "VANTAGE_ENVIRONMENT", "VANTAGE_COMPANY_ID", "VANTAGE_CLIENT_ID",
                 "SMS_BASE_URL", "SMS_SENDER", "OTP_EXPIRY_MINUTES", "OTP_MAX_REQUESTS", "COUNTER_TOKEN_HOURS",
                 "ADMIN_TOKEN_HOURS", "DEVICE_TOKEN_DAYS", "VARIANCE_TOLERANCE_PCT")
$generated   = @("DB_PASSWORD", "REDIS_PASSWORD", "JWT_SECRET", "OTP_SECRET")
$required    = @("SSH_HOST", "SSH_USER", "SSH_PRIVATE_KEY_FILE", "VANTAGE_TENANT_ID", "VANTAGE_ENVIRONMENT",
                 "VANTAGE_COMPANY_ID", "VANTAGE_CLIENT_ID", "VANTAGE_CLIENT_SECRET")

function Invoke-Gh {
  # Runs gh; returns its output; throws with gh's message on failure unless -AllowFail.
  param([string[]] $GhArgs, [string] $InputText, [switch] $AllowFail)
  # Windows PowerShell 5.1 turns a native program's stderr into errors, which "Stop" makes fatal:
  # here they are just output, and the exit code decides.
  $ErrorActionPreference = "Continue"
  if ($PSBoundParameters.ContainsKey("InputText")) { $out = $InputText | & gh @GhArgs 2>&1 } else { $out = & gh @GhArgs 2>&1 }
  $out = @($out | ForEach-Object { "$_" })
  if ($LASTEXITCODE -ne 0 -and -not $AllowFail) { throw "gh $($GhArgs[0..1] -join ' ') failed: $out" }
  return [pscustomobject]@{ Ok = ($LASTEXITCODE -eq 0); Output = ($out | Out-String) }
}

# Runs a native program with stderr dropped and not treated as an error (Windows PowerShell 5.1).
function Invoke-Quiet([scriptblock] $Block) {
  $ErrorActionPreference = "Continue"
  & $Block 2>$null
}

function New-RandomHex {
  $bytes = New-Object byte[] 32
  [System.Security.Cryptography.RandomNumberGenerator]::Create().GetBytes($bytes)
  -join ($bytes | ForEach-Object { $_.ToString("x2") })
}

function Read-EnvFile([string] $Path) {
  $values = [ordered]@{}
  foreach ($line in Get-Content -LiteralPath $Path) {
    if ($line -match '^\s*#' -or $line -notmatch '^\s*([A-Za-z_][A-Za-z0-9_]*)\s*=(.*)$') { continue }
    $v = $Matches[2].Trim()
    if ($v.Length -ge 2 -and (($v[0] -eq "'" -and $v[-1] -eq "'") -or ($v[0] -eq '"' -and $v[-1] -eq '"'))) {
      $v = $v.Substring(1, $v.Length - 2)
    }
    $values[$Matches[1]] = $v
  }
  return $values
}

function Set-EnvFileValue([string] $Path, [string] $Name, [string] $Value) {
  $lines = Get-Content -LiteralPath $Path
  $found = $false
  $lines = foreach ($line in $lines) {
    if ($line -match "^\s*$Name\s*=") { $found = $true; "$Name=$Value" } else { $line }
  }
  if (-not $found) { $lines += "$Name=$Value" }
  Set-Content -LiteralPath $Path -Value $lines -Encoding utf8
}

# The box's reverse proxy, as 3retail-licensing's setup does (EDGE_MODE=proxy): listening ports,
# and the nginx containers with their docker networks. Read-only. Returns $null when the box
# can't be reached. No double quotes on the ssh command line (Windows PowerShell 5.1 mangles them).
$script:BoxProbe = $null
function Get-BoxProbe($values, [string] $keyPath) {
  if ($script:BoxProbe) { return $script:BoxProbe }
  $ErrorActionPreference = "Continue"   # ssh's stderr is not an error here (see Invoke-Gh)
  $kh = [System.IO.Path]::GetTempFileName()
  try {
    Set-Content -LiteralPath $kh -Value $values["SSH_KNOWN_HOSTS"] -Encoding ascii
    $port = if ($values["SSH_PORT"]) { $values["SSH_PORT"] } else { "22" }
    $remote = 'ss -Hltn | awk ''{print $4}'' | sed -E ''s/.*:([0-9]+)$/\1/'' | sort -un | tr ''\n'' '' ''; echo; f=''{{.Names}}|{{.Image}}|{{.Networks}}''; docker ps --format $f 2>/dev/null || sudo -n docker ps --format $f 2>/dev/null || true'
    $out = & ssh -i $keyPath -o BatchMode=yes -o StrictHostKeyChecking=yes -o "UserKnownHostsFile=$kh" -o ConnectTimeout=15 `
             -p $port "$($values['SSH_USER'])@$($values['SSH_HOST'])" $remote 2>$null
    if ($LASTEXITCODE -ne 0 -or -not $out) { return $null }
  } finally { Remove-Item -LiteralPath $kh -ErrorAction SilentlyContinue }
  $lines = @($out)
  $ports = @("$($lines[0])" -split '\s+' | Where-Object { $_ -match '^\d+$' } | ForEach-Object { [int]$_ })
  $nginx = @(); $acme = $false
  foreach ($l in ($lines | Select-Object -Skip 1)) {
    $f = "$l" -split '\|'
    if ($f.Count -lt 3 -or $f[0] -like 'maflam-*') { continue }
    if ("$($f[0]) $($f[1])" -match 'acme-companion|letsencrypt-nginx-proxy-companion') { $acme = $true; continue }
    if ("$($f[0]) $($f[1])" -match 'nginx') { $nginx += ,@($f[0], $f[1], ($f[2] -split ',')[0]) }
  }
  $script:BoxProbe = [pscustomobject]@{ Ports = $ports; Nginx = $nginx; Acme = $acme }
  return $script:BoxProbe
}

# Prepares the box for one environment (as 3retail-licensing's setup does): Docker usable by the
# SSH user without sudo, rsync, and the environment's APP_DIR, created here when the user may
# (e.g. under its home). When something is still missing and the user has passwordless sudo,
# server-bootstrap.sh is copied over and run; otherwise it says what to run on the box.
function Initialize-Box($values, [string] $keyPath, [string] $appDir, [switch] $ReportOnly) {
  $ErrorActionPreference = "Continue"
  $user = $values["SSH_USER"]; $hostName = $values["SSH_HOST"]
  $port = if ($values["SSH_PORT"]) { $values["SSH_PORT"] } else { "22" }
  $kh = [System.IO.Path]::GetTempFileName()
  try {
    Set-Content -LiteralPath $kh -Value $values["SSH_KNOWN_HOSTS"] -Encoding ascii
    $opts = @("-i", $keyPath, "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes", "-o", "UserKnownHostsFile=$kh", "-o", "ConnectTimeout=15")
    $mk = if ($ReportOnly) { '' } else { "mkdir -p $appDir 2>/dev/null; " }
    $check = 'docker info >/dev/null 2>&1 && echo DOCKER_OK; command -v rsync >/dev/null 2>&1 && echo RSYNC_OK; ' +
             $mk + "[ -w $appDir ] && echo DIRS_OK; d=$appDir; while [ ! -e `$d ]; do d=`$(dirname `$d); done; [ ! -e $appDir ] && [ -w `$d ] && echo DIRS_CREATABLE; " +
             'sudo -n true >/dev/null 2>&1 && echo SUDO_OK; true'
    $out = (& ssh @opts -p $port "$user@$hostName" $check 2>$null | Out-String)
    if ($LASTEXITCODE -ne 0 -or -not $out) { Write-Warning "  could not check the box over SSH"; return $false }
    $need = @()
    if ($out -notmatch 'DOCKER_OK') { $need += "Docker for $user" }
    if ($out -notmatch 'RSYNC_OK')  { $need += "rsync" }
    if ($out -notmatch 'DIRS_OK') {
      if ($ReportOnly -and $out -match 'DIRS_CREATABLE') { Write-Host "  $appDir will be created (by $user, no sudo needed)" }
      else { $need += "$appDir writable by $user" }
    }
    if ($need.Count -eq 0) { Write-Host "  box ready: Docker for $user, rsync, $appDir"; return $true }
    Write-Host "  box needs: $($need -join ', ')"
    $manual = "on the box: sudo install -d -o $user -g $user $appDir   (or run deploy/maflam/server-bootstrap.sh --user $user there)"
    if ($ReportOnly) { Write-Host "  (not changed: -DryRun) setup-github.ps1 will run server-bootstrap.sh there"; return $false }
    if ($out -notmatch 'SUDO_OK') {
      Write-Warning "  $user has no passwordless sudo, so the box can't be prepared from here: $manual"
      return $false
    }
    $bootstrap = Join-Path $PSScriptRoot "server-bootstrap.sh"
    & scp @opts -P $port $bootstrap "${user}@${hostName}:/tmp/maflam-server-bootstrap.sh" 2>$null | Out-Null
    if ($LASTEXITCODE -ne 0) { Write-Warning "  could not copy server-bootstrap.sh to the box: $manual"; return $false }
    Write-Host "  running server-bootstrap.sh on the box (sudo)..."
    $run = (& ssh @opts -p $port "$user@$hostName" "sudo -n bash /tmp/maflam-server-bootstrap.sh --user $user; rc=`$?; rm -f /tmp/maflam-server-bootstrap.sh; exit `$rc" 2>&1 | ForEach-Object { "$_" })
    $run | Where-Object { $_ -match '^(\S|  )' } | Select-Object -Last 8 | ForEach-Object { Write-Host "    $_" }
    if ($LASTEXITCODE -ne 0) { Write-Warning "  server-bootstrap.sh failed on the box (output above): $manual"; return $false }
    Write-Host "  box prepared"
    return $true
  } finally { Remove-Item -LiteralPath $kh -ErrorAction SilentlyContinue }
}

# -- gh is there and signed in ------------------------------------------------
if (-not (Get-Command gh -ErrorAction SilentlyContinue)) {
  throw "gh (GitHub CLI) is not installed: winget install --id GitHub.cli, then gh auth login"
}
Invoke-Gh @("auth", "status") | Out-Null
Invoke-Gh @("repo", "view", $Repo, "--json", "name") | Out-Null
Write-Host "Repository: $Repo" -ForegroundColor Cyan

foreach ($target in $Targets) {
  $envName = "maflam-$target"
  $file    = Join-Path $serverRoot ".env.maflam-$target"
  Write-Host "`n-- $envName ($file)" -ForegroundColor Cyan

  if (-not (Test-Path -LiteralPath $file)) {
    Copy-Item -LiteralPath $template -Destination $file
    Write-Warning "Created $file from the template. Fill it in, then run this script again."
    continue
  }

  # Generate the server's own secrets once, and keep them in the file.
  $values = Read-EnvFile $file
  foreach ($name in $generated) {
    if ([string]::IsNullOrWhiteSpace($values[$name])) {
      $new = New-RandomHex
      if (-not $DryRun) { Set-EnvFileValue $file $name $new }
      $values[$name] = $new
      Write-Host "  generated $name (saved in the file)"
    }
  }

  # The deploy key: the file named by SSH_PRIVATE_KEY_FILE goes in as the SSH_PRIVATE_KEY secret.
  $missing = @($required | Where-Object { [string]::IsNullOrWhiteSpace($values[$_]) })
  $keyText = $null
  if ($values["SSH_PRIVATE_KEY_FILE"]) {
    $keyPath = $values["SSH_PRIVATE_KEY_FILE"] -replace '^~', $HOME
    if (-not [System.IO.Path]::IsPathRooted($keyPath)) { $keyPath = Join-Path $serverRoot $keyPath }
    if (Test-Path -LiteralPath $keyPath) { $keyText = (Get-Content -LiteralPath $keyPath -Raw).Trim() + "`n" }
    else { $missing += "SSH_PRIVATE_KEY_FILE (no file at $keyPath)" }
  }
  if ($missing.Count -gt 0) {
    Write-Warning "$envName skipped. Fill in: $($missing -join ', ')"
    continue
  }

  # Pin the server's host key when SSH_KNOWN_HOSTS is empty: from your own known_hosts first (you
  # have logged in there before), else ssh-keyscan (given 15 s; its "# host SSH-2.0-..." banner
  # line is not a key).
  if ([string]::IsNullOrWhiteSpace($values["SSH_KNOWN_HOSTS"])) {
    $port = if ($values["SSH_PORT"]) { $values["SSH_PORT"] } else { "22" }
    $hostRef = if ($port -eq "22") { $values["SSH_HOST"] } else { "[$($values['SSH_HOST'])]:$port" }
    $known = @()
    if (Get-Command ssh-keygen -ErrorAction SilentlyContinue) {
      $known = @(Invoke-Quiet { & ssh-keygen -F $hostRef } | ForEach-Object { "$_" } |
                 Where-Object { $_ -and -not $_.StartsWith("#") })
    }
    if ($known.Count -gt 0) {
      $values["SSH_KNOWN_HOSTS"] = ($known -join "`n")
      Write-Host "  host key of $hostRef taken from your known_hosts ($($known.Count) line(s))"
    } elseif (Get-Command ssh-keyscan -ErrorAction SilentlyContinue) {
      $scan = @(Invoke-Quiet { & ssh-keyscan -T 15 -p $port $values["SSH_HOST"] } | ForEach-Object { "$_" } |
                Where-Object { $_ -and -not $_.StartsWith("#") })
      if ($scan.Count -gt 0) {
        $values["SSH_KNOWN_HOSTS"] = ($scan -join "`n")
        Write-Host "  read the server's host key with ssh-keyscan (check it is the right server)"
      } else {
        Write-Warning "  no host key for $hostRef (not in your known_hosts; ssh-keyscan got none). Log in once with ssh -i <key> $($values['SSH_USER'])@$($values['SSH_HOST']) and run this again."
      }
    }
  }

  # Behind the nginx already on the box: its docker network (PROXY_NETWORK), or the host's nginx
  # and a local port. Found by looking at the box when PROXY_NETWORK is not set in the file.
  $probe = Get-BoxProbe $values $keyPath
  if (-not $probe) {
    Write-Warning "  could not look at the box over SSH (key, user or host key?): set PROXY_NETWORK by hand if nginx runs in docker"
  } else {
    foreach ($n in $probe.Nginx) { Write-Host "  found $($n[0]) ($($n[1])) on the docker network '$($n[2])'" }
    if ([string]::IsNullOrWhiteSpace($values["PROXY_NETWORK"])) {
      $docked = @($probe.Nginx | Where-Object { $_[2] -notin @("host", "bridge") })
      $preferred = @($docked | Where-Object { $_[1] -match 'nginx-proxy' })
      $pick = if ($preferred.Count -ge 1) { $preferred[0] } elseif ($docked.Count -eq 1) { $docked[0] } else { $null }
      if ($pick) {
        $values["PROXY_NETWORK"] = $pick[2]
        if (-not $DryRun) { Set-EnvFileValue $file "PROXY_NETWORK" $pick[2] }
        $saved = if ($DryRun) { "would be saved in the file" } else { "saved in the file" }
        Write-Host "  PROXY_NETWORK=$($pick[2]) ($($pick[0])), ${saved}: the stack joins it, no host port"
      } elseif ($docked.Count -gt 1) {
        Write-Warning "  several nginx containers on docker networks: set PROXY_NETWORK in the file to the one serving 80/443"
      } else {
        $hp = if ($values["HTTP_PORT"]) { [int]$values["HTTP_PORT"] } elseif ($target -eq "live") { 8084 } else { 8085 }
        if ($probe.Ports -contains $hp) { Write-Warning "  port $hp is already in use on the box: set another HTTP_PORT in the file" }
        Write-Host "  nginx on the host: add a server block for $($values['PUBLIC_URL']) that proxies to http://127.0.0.1:$hp"
      }
    }
    if ($values["PROXY_NETWORK"]) {
      $le = if ($probe.Acme) { "acme-companion found: the deploy sets LETSENCRYPT_HOST (https certificate on the box)" } else { "no acme-companion: nginx-proxy serves the name over plain http (HTTPS_METHOD=nohttps)" }
      Write-Host "  $le"
    }
  }

  # The box itself: folders, Docker for the SSH user, rsync (server-bootstrap.sh when needed).
  $appDir = if ($values["APP_DIR"]) { $values["APP_DIR"] } else { "/opt/stocktake/$target" }
  if ($values["SSH_KNOWN_HOSTS"]) { [void](Initialize-Box $values $keyPath $appDir -ReportOnly:$DryRun) }

  if ($DryRun) {
    Write-Host "  would create/update environment $envName"
    Write-Host "  secrets:   SSH_PRIVATE_KEY $(@($secretNames | Where-Object { $values[$_] }) -join ' ')"
    Write-Host "  variables: $(@($varNames | Where-Object { $values[$_] }) -join ' ')"
    continue
  }

  # -- The environment: main only; live also tries a required approver ---------
  $body = @{ deployment_branch_policy = @{ protected_branches = $false; custom_branch_policies = $true } }
  if ($target -eq "live" -and $RequireApproval) {
    $login = if ($LiveReviewer) { $LiveReviewer } else { (Invoke-Gh @("api", "user", "--jq", ".login")).Output.Trim() }
    $userId = [int64](Invoke-Gh @("api", "users/$login", "--jq", ".id")).Output.Trim()
    $withReviewer = $body.Clone()
    $withReviewer.reviewers = @(@{ type = "User"; id = $userId })
    $r = Invoke-Gh @("api", "-X", "PUT", "repos/$Repo/environments/$envName", "--input", "-") `
         -InputText ($withReviewer | ConvertTo-Json -Depth 5) -AllowFail
    if ($r.Ok) {
      Write-Host "  environment ${envName}: main only, $login must approve each deploy"
    } else {
      Write-Warning "  GitHub refused a required approver ($($r.Output.Trim())). That needs GitHub Enterprise for a private repository; live is still manual-only and main-only."
      Invoke-Gh @("api", "-X", "PUT", "repos/$Repo/environments/$envName", "--input", "-") `
        -InputText ($body | ConvertTo-Json -Depth 5) | Out-Null
    }
  } else {
    # No approver (an existing one is removed: reviewers is set to an empty list).
    $body.reviewers = @()
    Invoke-Gh @("api", "-X", "PUT", "repos/$Repo/environments/$envName", "--input", "-") `
      -InputText ($body | ConvertTo-Json -Depth 5) | Out-Null
    # GitHub's docs don't say what an empty reviewers list does to existing ones: read it back.
    $rules = (Invoke-Gh @("api", "repos/$Repo/environments/$envName", "--jq", ".protection_rules[]?.type") -AllowFail).Output.Trim()
    if ($rules -match 'required_reviewers') {
      Write-Warning "  ${envName} still has a required approver: remove it in GitHub, Settings -> Environments -> $envName -> Required reviewers"
    } else {
      Write-Host "  environment ${envName}: main only, no approval needed"
    }
  }
  $policies = (Invoke-Gh @("api", "repos/$Repo/environments/$envName/deployment-branch-policies",
               "--jq", ".branch_policies[].name")).Output
  if ($policies -notmatch '(?m)^main\s*$') {
    Invoke-Gh @("api", "-X", "POST", "repos/$Repo/environments/$envName/deployment-branch-policies",
                "-f", "name=main", "-f", "type=branch") | Out-Null
  }

  # -- Secrets (sent on stdin, never on the command line) and variables ---------
  Invoke-Gh @("secret", "set", "SSH_PRIVATE_KEY", "--env", $envName, "--repo", $Repo) -InputText $keyText | Out-Null
  $setS = @("SSH_PRIVATE_KEY")
  foreach ($name in $secretNames) {
    if ([string]::IsNullOrWhiteSpace($values[$name])) { continue }
    Invoke-Gh @("secret", "set", $name, "--env", $envName, "--repo", $Repo) -InputText $values[$name] | Out-Null
    $setS += $name
  }
  $setV = @()
  foreach ($name in $varNames) {
    if ([string]::IsNullOrWhiteSpace($values[$name])) { continue }
    Invoke-Gh @("variable", "set", $name, "--env", $envName, "--repo", $Repo, "--body", $values[$name]) | Out-Null
    $setV += $name
  }
  Write-Host "  secrets:   $($setS -join ' ')"
  Write-Host "  variables: $($setV -join ' ')"
  $empty = @(@("SMS_BASE_URL", "SMS_API_KEY", "PUBLIC_URL") | Where-Object { [string]::IsNullOrWhiteSpace($values[$_]) })
  if ($empty.Count -gt 0) { Write-Warning "  not set (the server runs without them): $($empty -join ', ')" }
}

Write-Host "`nDone. Check: gh api repos/$Repo/environments --jq '.environments[].name'" -ForegroundColor Green
