# Offline Windows entry point. Requires a PREPROVISIONED WSL distribution with
# Python + cryptography + e2fsprogs + ARM compiler, and this checkout on a local drive.
# No device, SSH, networking, mounting, admin rights or EngineX used by this script.
[CmdletBinding()]
param(
    [Parameter(Mandatory=$true)][string]$PublicKey,
    [Parameter(Mandatory=$true)][string]$Output,
    [Parameter(Mandatory=$true)][long]$SizeBytes,
    [string]$Distribution = 'Debian'
)
$ErrorActionPreference = 'Stop'
$Repo = Split-Path $PSScriptRoot -Parent
function LinuxPath([string]$Path) {
    $result = & wsl.exe -d $Distribution -- wslpath -a -u $Path
    if ($LASTEXITCODE -ne 0) { throw 'WSL path conversion failed' }
    return $result.Trim()
}
$RepoLinux = LinuxPath $Repo
$KeyLinux = LinuxPath (Resolve-Path -LiteralPath $PublicKey).Path
$OutputLinux = LinuxPath ([IO.Path]::GetFullPath($Output))
$HookLinux = "$OutputLinux.hook.so"
# Argument arrays avoid interpolating user paths into a shell command.
& wsl.exe -d $Distribution --cd $RepoLinux -- sh tools/build-jc11s-hook.sh $HookLinux
if ($LASTEXITCODE -ne 0) { throw 'ARM hook build failed' }
& wsl.exe -d $Distribution --cd $RepoLinux -- python3 -m core.jc11s_image build --key $KeyLinux --hook $HookLinux --size-bytes $SizeBytes --output $OutputLinux
if ($LASTEXITCODE -ne 0) { throw 'Image build or offline verification failed' }
