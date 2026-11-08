param(
    [string]$OutDir = $PSScriptRoot
)

Add-Type -AssemblyName System.Drawing

function New-RoundedPath([double]$x, [double]$y, [double]$w, [double]$h, [double]$r) {
    if ($r -lt 0) { $r = 0 }
    $max = [Math]::Min($w, $h) / 2
    if ($r -gt $max) { $r = $max }
    $d = $r * 2
    $path = New-Object System.Drawing.Drawing2D.GraphicsPath
    $path.AddArc([float]$x, [float]$y, [float]$d, [float]$d, 180, 90)
    $path.AddArc([float]($x + $w - $d), [float]$y, [float]$d, [float]$d, 270, 90)
    $path.AddArc([float]($x + $w - $d), [float]($y + $h - $d), [float]$d, [float]$d, 0, 90)
    $path.AddArc([float]$x, [float]($y + $h - $d), [float]$d, [float]$d, 90, 90)
    $path.CloseFigure()
    return $path
}

function New-IconBitmap([int]$size) {
    $s = $size / 256.0
    # Small sizes get a slightly larger N so the diagonal and strokes stay clear.
    if ($size -le 20) {
        $nScale = 1.08
    } elseif ($size -le 24) {
        $nScale = 1.04
    } else {
        $nScale = 1.0
    }

    $bmp = New-Object System.Drawing.Bitmap($size, $size, [System.Drawing.Imaging.PixelFormat]::Format32bppArgb)
    $g = [System.Drawing.Graphics]::FromImage($bmp)
    $g.SmoothingMode = [System.Drawing.Drawing2D.SmoothingMode]::AntiAlias
    $g.PixelOffsetMode = [System.Drawing.Drawing2D.PixelOffsetMode]::HighQuality
    $g.CompositingQuality = [System.Drawing.Drawing2D.CompositingQuality]::HighQuality
    $g.Clear([System.Drawing.Color]::Transparent)

    $tileBrush = New-Object System.Drawing.SolidBrush([System.Drawing.ColorTranslator]::FromHtml('#17181C'))
    $tile = New-RoundedPath (1 * $s) (1 * $s) (254 * $s) (254 * $s) (56 * $s)
    $g.FillPath($tileBrush, $tile)

    function Point([double]$x, [double]$y) {
        return New-Object System.Drawing.PointF(
            [float]((($x - 128) * $nScale + 128) * $s),
            [float]((($y - 128) * $nScale + 128) * $s))
    }

    function Draw-Segment([double]$x1, [double]$y1, [double]$x2, [double]$y2, [string]$c1, [string]$c2) {
        $p1 = Point $x1 $y1
        $p2 = Point $x2 $y2
        $brush = New-Object System.Drawing.Drawing2D.LinearGradientBrush(
            $p1, $p2,
            [System.Drawing.ColorTranslator]::FromHtml($c1),
            [System.Drawing.ColorTranslator]::FromHtml($c2))
        $pen = New-Object System.Drawing.Pen($brush, [float](48 * $s * $nScale))
        $pen.StartCap = [System.Drawing.Drawing2D.LineCap]::Round
        $pen.EndCap = [System.Drawing.Drawing2D.LineCap]::Round
        $pen.LineJoin = [System.Drawing.Drawing2D.LineJoin]::Round
        $g.DrawLine($pen, $p1, $p2)
        $pen.Dispose()
        $brush.Dispose()
    }

    Draw-Segment 82 176 82 78 '#7C4DFF' '#2E90FF'
    Draw-Segment 82 78 174 176 '#2E90FF' '#22D3EE'
    Draw-Segment 174 176 174 90 '#22D3EE' '#12BFCB'

    $tile.Dispose()
    $tileBrush.Dispose()
    $g.Dispose()
    return $bmp
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

$sizes = @(16, 20, 24, 32, 48, 64, 128, 256)
$frames = @()
foreach ($size in $sizes) {
    $bmp = New-IconBitmap $size
    if ($size -ge 256) {
        $data = ConvertTo-IcoPng $bmp
    } else {
        $data = ConvertTo-IcoDib $bmp
    }
    $bmp.Dispose()
    $frames += [pscustomobject]@{ Size = $size; Data = $data }
}
Write-Ico (Join-Path $OutDir 'icon.ico') $frames

$png = New-IconBitmap 256
$png.Save((Join-Path $OutDir 'icon.png'), [System.Drawing.Imaging.ImageFormat]::Png)
$png.Dispose()

$png = New-IconBitmap 512
$png.Save((Join-Path $OutDir 'icon_512.png'), [System.Drawing.Imaging.ImageFormat]::Png)
$png.Dispose()

Write-Output 'wrote icon.ico, icon.png, icon_512.png'
