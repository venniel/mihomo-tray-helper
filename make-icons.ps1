param(
    [string]$OutDir = $PSScriptRoot
)

$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Drawing

$resvg = Get-Command resvg-cli -ErrorAction SilentlyContinue
if (-not $resvg) {
    $resvg = Get-Command resvg-js -ErrorAction SilentlyContinue
}
if (-not $resvg) {
    throw 'resvg-cli is required to rasterize icon.svg. Install it with: npm install -g resvg-cli'
}

$svg = Join-Path $PSScriptRoot 'icon.svg'
$tempDir = Join-Path ([System.IO.Path]::GetTempPath()) ('mth-icons-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Force -Path $tempDir | Out-Null

function Get-FramePath([int]$size) {
    return Join-Path $tempDir "$size.png"
}

foreach ($size in @(16, 20, 24, 32, 48, 64, 128, 256, 512)) {
    $output = Get-FramePath $size
    & $resvg.Source --log-level error --fit-width $size $svg $output | Out-Null
    if ($LASTEXITCODE -ne 0) {
        throw "resvg-cli failed to render icon.svg at $size px."
    }
}

function ConvertTo-IcoDib([System.Drawing.Bitmap]$bmp) {
    $w = $bmp.Width
    $h = $bmp.Height
    $xorBytes = $w * 4
    $maskStride = [int]([math]::Floor(($w + 31) / 32) * 4)
    $maskSize = $maskStride * $h
    $xorSize = $xorBytes * $h

    $rect = New-Object System.Drawing.Rectangle(0, 0, $w, $h)
    $locked = $bmp.LockBits($rect, [System.Drawing.Imaging.ImageLockMode]::ReadOnly, [System.Drawing.Imaging.PixelFormat]::Format32bppArgb)
    $pixels = New-Object byte[] ($locked.Stride * $h)
    [System.Runtime.InteropServices.Marshal]::Copy($locked.Scan0, $pixels, 0, $pixels.Length)
    $stride = $locked.Stride
    $bmp.UnlockBits($locked)

    $stream = New-Object System.IO.MemoryStream
    $writer = New-Object System.IO.BinaryWriter($stream)
    $writer.Write([uint32]40)
    $writer.Write([int32]$w)
    $writer.Write([int32]($h * 2))
    $writer.Write([uint16]1)
    $writer.Write([uint16]32)
    $writer.Write([uint32]0)
    $writer.Write([uint32]$xorSize)
    $writer.Write([int32]0)
    $writer.Write([int32]0)
    $writer.Write([uint32]0)
    $writer.Write([uint32]0)

    for ($y = $h - 1; $y -ge 0; $y--) {
        $writer.Write($pixels, $y * $stride, $xorBytes)
    }

    $mask = New-Object byte[] $maskSize
    $writer.Write($mask)

    $writer.Close()
    $data = $stream.ToArray()
    $stream.Dispose()
    return $data
}

function ConvertTo-IcoPng([System.Drawing.Bitmap]$bmp) {
    $stream = New-Object System.IO.MemoryStream
    $bmp.Save($stream, [System.Drawing.Imaging.ImageFormat]::Png)
    $data = $stream.ToArray()
    $stream.Dispose()
    return $data
}

function Write-Ico([string]$path, [object[]]$frames) {
    $stream = [System.IO.File]::Create($path)
    $writer = New-Object System.IO.BinaryWriter($stream)
    $writer.Write([uint16]0)
    $writer.Write([uint16]1)
    $writer.Write([uint16]$frames.Count)

    $offset = 6 + 16 * $frames.Count
    foreach ($frame in $frames) {
        $dim = if ($frame.Size -ge 256) { 0 } else { $frame.Size }
        $writer.Write([byte]$dim)
        $writer.Write([byte]$dim)
        $writer.Write([byte]0)
        $writer.Write([byte]0)
        $writer.Write([uint16]1)
        $writer.Write([uint16]32)
        $writer.Write([uint32]$frame.Data.Length)
        $writer.Write([uint32]$offset)
        $offset += $frame.Data.Length
    }
    foreach ($frame in $frames) {
        $writer.Write([byte[]]$frame.Data)
    }
    $writer.Close()
    $stream.Close()
}

try {
    $sizes = @(16, 20, 24, 32, 48, 64, 128, 256)
    $frames = @()
    foreach ($size in $sizes) {
        $bmp = [System.Drawing.Bitmap]::FromFile((Get-FramePath $size))
        if ($size -ge 256) {
            $data = ConvertTo-IcoPng $bmp
        } else {
            $data = ConvertTo-IcoDib $bmp
        }
        $bmp.Dispose()
        $frames += [pscustomobject]@{ Size = $size; Data = $data }
    }
    Write-Ico (Join-Path $OutDir 'icon.ico') $frames

    $assetsDir = Join-Path $OutDir 'assets'
    New-Item -ItemType Directory -Force -Path $assetsDir | Out-Null

    $bmp = [System.Drawing.Bitmap]::FromFile((Get-FramePath 256))
    $bmp.Save((Join-Path $assetsDir 'icon.png'), [System.Drawing.Imaging.ImageFormat]::Png)
    $bmp.Dispose()

    $bmp = [System.Drawing.Bitmap]::FromFile((Get-FramePath 512))
    $bmp.Save((Join-Path $assetsDir 'icon_512.png'), [System.Drawing.Imaging.ImageFormat]::Png)
    $bmp.Dispose()

    Write-Output 'wrote icon.ico, assets/icon.png, assets/icon_512.png'
} finally {
    Remove-Item -Recurse -Force $tempDir -ErrorAction SilentlyContinue
}
