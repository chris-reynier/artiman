<#
.SYNOPSIS
  Builds the ArtiMan Web distribution ZIP.

.DESCRIPTION
  - Runs the tests.
  - Embeds the icon and version information (go-winres).
  - Builds a 32-bit, console-less ArtiMan.exe (runs on 32- and 64-bit Windows 10/11).
  - Assembles the distribution with the user guide and, optionally, the private data of the business.
  - Zips everything to dist\ArtiMan-<version>.zip.

  The repository holds no business identity and no customer data. Those live in a private folder outside the
  repository, passed with -PrivateDir. Every file in it is optional:

    settings.json   company identity and other settings, e.g. {"company_name": "...", "company_siret": "..."}
                    (keys: see internal/store/settings.go)
    logo.png        letterhead logo
    history.json    legacy history (data.json produced by tools/legacy-extraction)
    description.txt description library of the old ArtiMan
    documents\      original legacy .doc files (same relative paths as in history.json)

  The first run of ArtiMan.exe applies them once.

.EXAMPLE
  .\tools\build.ps1 -Version 1.0.0 -PrivateDir C:\path\to\artiman-private
#>
param(
    [string]$Version = "1.0.0",
    [string]$PrivateDir = "",
    [string]$Go = "go",
    [switch]$SkipTests
)
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
Set-Location $root
$env:GOTOOLCHAIN = "local"

if ($PrivateDir) {
    $PrivateDir = (Resolve-Path $PrivateDir).Path
    if ($PrivateDir.StartsWith($root, [StringComparison]::OrdinalIgnoreCase)) { throw "PrivateDir must be outside the repository" }
}

if (-not $SkipTests) {
    & $Go test ./...
    if ($LASTEXITCODE -ne 0) { throw "tests failed" }
}

$copyright = "ArtiMan"
$settingsFile = if ($PrivateDir) { Join-Path $PrivateDir "settings.json" } else { "" }
if ($settingsFile -and (Test-Path $settingsFile)) {
    $name = (Get-Content $settingsFile -Raw -Encoding UTF8 | ConvertFrom-Json).company_name
    if ($name) { $copyright = $name }
}

$winres = Join-Path (& $Go env GOPATH) "bin\go-winres.exe"
if (-not (Test-Path $winres)) { & $Go install github.com/tc-hib/go-winres@latest }
& $winres simply --arch "386,amd64" --out "cmd\artiman\rsrc" --icon "web\static\img\icon.png" --manifest gui `
    --product-name "ArtiMan" --product-version $Version --file-version $Version `
    --file-description "ArtiMan - devis et factures" --copyright $copyright --original-filename "ArtiMan.exe"
if ($LASTEXITCODE -ne 0) { throw "go-winres failed" }

$dist = Join-Path $root "dist"
# Assemble in a staging folder that only this script uses: never delete a folder someone may be running
# ArtiMan from (it would hold real data).
$stage = Join-Path $dist "staging"
$out = Join-Path $stage "ArtiMan"
if (Test-Path (Join-Path $out "data\artiman.db")) { throw "$out contient une base de données : build annulé" }
if (Test-Path $stage) { Remove-Item $stage -Recurse -Force }
New-Item -ItemType Directory -Force $out | Out-Null

$env:GOOS = "windows"; $env:GOARCH = "386"; $env:CGO_ENABLED = "0"
& $Go build -trimpath -ldflags "-s -w -H windowsgui -X main.version=$Version" -o (Join-Path $out "ArtiMan.exe") ./cmd/artiman
if ($LASTEXITCODE -ne 0) { throw "build failed" }
Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED

Copy-Item "docs\LISEZMOI.html" $out
Copy-Item "LICENSE" (Join-Path $out "LICENCE.txt")
# Demo launcher (fictitious data in demo-data, never mixed with the real data)
Set-Content -Path (Join-Path $out "Essayer la demo.cmd") -Encoding Ascii -Value "@echo off`r`nstart `"`" `"%~dp0ArtiMan.exe`" -demo"

if ($PrivateDir) {
    $import = Join-Path $out "data\import"
    New-Item -ItemType Directory -Force $import | Out-Null
    foreach ($f in "settings.json", "logo.png", "history.json", "description.txt") {
        $p = Join-Path $PrivateDir $f
        if (Test-Path $p) { Copy-Item $p $import; "  + $f" }
    }
    $docs = Join-Path $PrivateDir "documents"
    if (Test-Path $docs) {
        $legacy = Join-Path $out "data\legacy"
        New-Item -ItemType Directory -Force $legacy | Out-Null
        Copy-Item (Join-Path $docs "*") $legacy -Recurse
        "  + documents ({0} fichiers)" -f (Get-ChildItem $legacy -Recurse -File).Count
    }
}

$zip = Join-Path $dist "ArtiMan-$Version.zip"
if (Test-Path $zip) { Remove-Item $zip -Force }
Compress-Archive -Path $out -DestinationPath $zip -CompressionLevel Optimal
$exe = Get-Item (Join-Path $out "ArtiMan.exe")
"ArtiMan.exe : {0:N1} Mo" -f ($exe.Length / 1MB)
"ZIP : {0} ({1:N1} Mo)" -f $zip, ((Get-Item $zip).Length / 1MB)
