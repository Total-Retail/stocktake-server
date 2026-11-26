<#
.SYNOPSIS
  Lists the companies (id and name) in a Business Central environment, with the app registration
  from a .env.maflam-* file - to find VANTAGE_COMPANY_ID for live.

.EXAMPLE
  .\deploy\maflam\list-bc-companies.ps1 -Environment Production
  .\deploy\maflam\list-bc-companies.ps1 -Environment Production -EnvFile .env.maflam-live

  Uses VANTAGE_TENANT_ID, VANTAGE_CLIENT_ID and VANTAGE_CLIENT_SECRET from the file (default
  .env.maflam-testing). Nothing is written; the secret is never printed. A 401/403 means that app
  registration is not set up in that environment (Microsoft Entra Applications in BC, with
  Vantage Retail - All) - then live needs its own client id and secret.
#>
[CmdletBinding()]
param(
  [Parameter(Mandatory)] [string] $Environment,
  [string] $EnvFile = ".env.maflam-testing"
)
$ErrorActionPreference = "Stop"
$serverRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path
if (-not [System.IO.Path]::IsPathRooted($EnvFile)) { $EnvFile = Join-Path $serverRoot $EnvFile }

$v = @{}
foreach ($line in Get-Content -LiteralPath $EnvFile) {
  if ($line -match '^\s*(VANTAGE_TENANT_ID|VANTAGE_CLIENT_ID|VANTAGE_CLIENT_SECRET)\s*=(.*)$') { $v[$Matches[1]] = $Matches[2].Trim() }
}
foreach ($k in "VANTAGE_TENANT_ID", "VANTAGE_CLIENT_ID", "VANTAGE_CLIENT_SECRET") {
  if (-not $v[$k]) { throw "$k is empty in $EnvFile" }
}

[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
$token = (Invoke-RestMethod -Method Post `
  -Uri "https://login.microsoftonline.com/$($v.VANTAGE_TENANT_ID)/oauth2/v2.0/token" `
  -Body @{
    grant_type    = "client_credentials"
    client_id     = $v.VANTAGE_CLIENT_ID
    client_secret = $v.VANTAGE_CLIENT_SECRET
    scope         = "https://api.businesscentral.dynamics.com/.default"
  }).access_token

$uri = "https://api.businesscentral.dynamics.com/v2.0/$($v.VANTAGE_TENANT_ID)/$([uri]::EscapeDataString($Environment))/api/v2.0/companies"
try {
  $res = Invoke-RestMethod -Uri $uri -Headers @{ Authorization = "Bearer $token" }
} catch {
  $code = $_.Exception.Response.StatusCode.value__
  throw "Business Central answered $code for environment '$Environment'. 404: no environment by that name in this tenant. 401/403: this app registration is not allowed there."
}
Write-Host "Companies in '$Environment' (tenant $($v.VANTAGE_TENANT_ID), app $($v.VANTAGE_CLIENT_ID)):"
$res.value | Select-Object id, name, displayName | Format-Table -AutoSize
