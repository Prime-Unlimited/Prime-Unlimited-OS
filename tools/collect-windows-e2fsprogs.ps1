param(
    [Parameter(Mandatory = $true)][string]$Source,
    [Parameter(Mandatory = $true)][string]$Destination
)

$ErrorActionPreference = "Stop"
$bin = Join-Path $Destination "bin"
$etc = Join-Path $Destination "etc"
$licenses = Join-Path $Destination "licenses"
New-Item -ItemType Directory -Force $bin, $etc, $licenses | Out-Null

$objdump = Join-Path $Source "bin\objdump.exe"
$queue = [System.Collections.Generic.Queue[string]]::new()
@("mke2fs.exe", "debugfs.exe", "e2fsck.exe") | ForEach-Object { $queue.Enqueue($_) }
$copied = @{}
while ($queue.Count -gt 0) {
    $name = $queue.Dequeue()
    if ($copied.ContainsKey($name.ToLowerInvariant())) { continue }
    $sourceFile = Join-Path $Source "bin\$name"
    if (-not (Test-Path $sourceFile)) { throw "Required MinGW file is missing: $sourceFile" }
    Copy-Item $sourceFile $bin
    $copied[$name.ToLowerInvariant()] = $true
    & $objdump -p $sourceFile | Select-String "DLL Name:" | ForEach-Object {
        $dependency = ($_.Line -split "DLL Name:", 2)[1].Trim()
        if ((Test-Path (Join-Path $Source "bin\$dependency")) -and
            -not $copied.ContainsKey($dependency.ToLowerInvariant())) {
            $queue.Enqueue($dependency)
        }
    }
}

$config = @(
    (Join-Path $Source "etc\mke2fs.conf"),
    "C:\msys64\etc\mke2fs.conf"
) | Where-Object { Test-Path $_ } | Select-Object -First 1
if (-not $config) { throw "mke2fs.conf was not found in the MSYS2 installation" }
Copy-Item $config (Join-Path $etc "mke2fs.conf")

$licenseRoot = Join-Path $Source "share\licenses\e2fsprogs"
if (Test-Path $licenseRoot) { Copy-Item "$licenseRoot\*" $licenses -Recurse }
@(
    (Join-Path $Source "usr\share\doc\e2fsprogs"),
    (Join-Path $Source "usr\share\doc\Cygwin\e2fsprogs.README"),
    (Join-Path $Source "usr\share\doc\Cygwin\cygwin-doc.README"),
    (Join-Path $Source "COPYING")
) | Where-Object { Test-Path $_ } | ForEach-Object { Copy-Item $_ $licenses -Recurse -Force }

