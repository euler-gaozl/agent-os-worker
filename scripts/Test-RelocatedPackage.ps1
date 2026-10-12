param([Parameter(Mandatory)][string]$Package)
$ErrorActionPreference='Stop'
$source=(Resolve-Path -LiteralPath $Package).Path
$parent=Split-Path -Parent $source
$target=Join-Path $parent ((Split-Path -Leaf $source)+'-relocated-'+[guid]::NewGuid().ToString('N'))
if(-not(Test-Path -LiteralPath (Join-Path $source 'package.json'))){throw 'Only a generated Worker package may be relocated.'}
$manifest=Get-Content -LiteralPath (Join-Path $source 'package.json') -Raw|ConvertFrom-Json
if($manifest.format -ne 1 -or $manifest.role -cne 'worker' -or $manifest.dependency_layout -cne 'hoisted-link-free'){throw 'Unexpected package contract.'}
# Both resolved absolute paths must be siblings under the explicit package's
# parent. Do not recursively move a guessed workspace or overwrite anything.
if([IO.Path]::GetFullPath((Split-Path -Parent $target)) -cne [IO.Path]::GetFullPath($parent) -or (Test-Path -LiteralPath $target)){throw 'Unsafe relocation target.'}
Move-Item -LiteralPath $source -Destination $target
if(Test-Path -LiteralPath $source){throw 'Original package root is still present.'}
if(@(Get-ChildItem -LiteralPath $target -Recurse -Force|Where-Object{$_.Attributes -band [IO.FileAttributes]::ReparsePoint}).Count){throw 'Relocated package contains a filesystem link.'}
foreach($file in $manifest.files){
 $path=[IO.Path]::GetFullPath((Join-Path $target $file.path))
 if(-not $path.StartsWith($target+[IO.Path]::DirectorySeparatorChar,[StringComparison]::Ordinal)){throw 'Manifest escaped package root.'}
 if((Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash.ToLowerInvariant() -cne $file.sha256){throw 'Relocated package checksum mismatch.'}
}
$node=Join-Path $target ('node/'+$(if($IsWindows){'node.exe'}else{'node'}))
& $node (Join-Path $PSScriptRoot 'kernel-smoke.mjs') $target
if($LASTEXITCODE){throw 'Relocated kernel failed.'}
Write-Output ('Relocation verified with original root absent: '+$target)
