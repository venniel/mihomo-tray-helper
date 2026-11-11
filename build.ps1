$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot

$goversioninfo = Get-Command goversioninfo -ErrorAction SilentlyContinue
if ($goversioninfo) {
    & $goversioninfo.Source -64 -icon icon.ico -manifest mth.manifest -o resource_windows_amd64.syso versioninfo.json
} elseif (-not (Test-Path resource_windows_amd64.syso)) {
    throw 'resource_windows_amd64.syso is missing and goversioninfo is not installed.'
}

go build -ldflags '-H=windowsgui -s -w' -trimpath -o MTH.exe .
