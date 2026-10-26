# Reproducible build of focuskeeper.exe: the same source and the same Go version give the same bytes, wherever the
# folder lies and whether it is a git clone or an unpacked archive (-trimpath, no VCS stamp, an empty build id,
# no symbol table). Writes <Out>\focuskeeper.exe and <Out>\SHA256SUMS.txt.
#   powershell -ExecutionPolicy Bypass -File build.ps1 [-Version v1.0.0] [-Out release]
# The release workflow (.github/workflows/release.yml) runs this same script with the same Go version.
param([string]$Version = "dev", [string]$Out = "release")
$ErrorActionPreference = "Stop"
$GoVersion = "go1.27.2" # pinned: another Go version gives a different exe (and hash)

Set-Location $PSScriptRoot
$have = (& go env GOVERSION)
if ($have -ne $GoVersion) {
  Write-Warning "Go is ${have}, the pinned version is ${GoVersion}: the exe will work but its hash will differ from the release."
}
$env:CGO_ENABLED = "0"
$env:GOOS = "windows"
$env:GOARCH = "amd64"
New-Item -ItemType Directory -Force $Out | Out-Null
$exe = Join-Path $Out "focuskeeper.exe"
& go build -trimpath -buildvcs=false -ldflags "-s -w -H windowsgui -buildid= -X main.version=$Version" -o $exe .
if ($LASTEXITCODE -ne 0) { throw "go build failed" }
$hash = (Get-FileHash -Algorithm SHA256 $exe).Hash.ToLower()
# the sha256sum format: "<hash>  <name>", LF line ends - `sha256sum -c SHA256SUMS.txt` checks it too
[System.IO.File]::WriteAllText((Join-Path (Resolve-Path $Out) "SHA256SUMS.txt"), "$hash  focuskeeper.exe`n")
"$hash  $exe ($have, version $Version)"
