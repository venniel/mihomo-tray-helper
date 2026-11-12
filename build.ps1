$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot

$rsrc = Get-Command rsrc -ErrorAction SilentlyContinue
if ($rsrc) {
    & $rsrc.Source -arch amd64 -ico icon.ico -o resource_windows_amd64.syso
} elseif (-not (Test-Path resource_windows_amd64.syso)) {
    throw 'resource_windows_amd64.syso is missing and rsrc is not installed.'
}

go build -ldflags '-H=windowsgui -s -w' -trimpath -o MTH.exe .
