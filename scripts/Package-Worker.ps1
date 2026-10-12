param(
 [Parameter(Mandatory)][string]$Destination,
 [string]$Node='node',
 [string]$Pnpm='pnpm',
 [string]$StoreDirectory='',
 [switch]$Offline
)
$ErrorActionPreference='Stop'
$root=[IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$target=[IO.Path]::GetFullPath($Destination)
if(Test-Path -LiteralPath $target){throw 'Package destination must be new; do not merge with an existing release.'}
if($Offline -and -not $StoreDirectory){throw 'Offline packaging requires a retained pnpm store.'}
$dirty=@(& git -C $root status --porcelain)
if($LASTEXITCODE -or $dirty.Count){throw 'Release packaging requires a clean Git checkout.'}
$commit=(& git -C $root rev-parse HEAD).Trim()
if($LASTEXITCODE -or $commit -notmatch '^[0-9a-f]{40}$'){throw 'Cannot determine source commit.'}
$nodePath=(Get-Command $Node -ErrorAction Stop).Source
$version=(& $nodePath --version).Trim()
if($LASTEXITCODE -or $version -cne 'v24.19.0'){throw 'This release package pins Node 24.19.0.'}
$pnpmPath=(Get-Command $Pnpm -ErrorAction Stop).Source
$pnpmVersion=(& $pnpmPath --version).Trim()
if($LASTEXITCODE -or $pnpmVersion -ne '11.25.0'){throw 'Use pinned pnpm 11.25.0.'}
New-Item -ItemType Directory -Path $target|Out-Null
& (Join-Path $PSScriptRoot 'Build.ps1') -Destination (Join-Path $target 'bin')
$nodeTarget=(& $nodePath -p 'JSON.stringify({os:process.platform,arch:process.arch})')|ConvertFrom-Json
if($LASTEXITCODE){throw 'Cannot inspect selected Node target.'}
$expectedOS=@{win32='windows';linux='linux';darwin='darwin'}[$nodeTarget.os]
$expectedArch=@{x64='amd64';arm64='arm64'}[$nodeTarget.arch]
if(-not $expectedOS -or -not $expectedArch){throw 'Unsupported Node package target.'}
$binaryName=if($IsWindows){'worker.exe'}else{'worker'}
$info=(& go version -m -json (Join-Path $target ('bin/'+$binaryName)))|ConvertFrom-Json
if($LASTEXITCODE){throw 'Cannot inspect Worker package target.'}
$settings=@{};foreach($setting in $info.Settings){$settings[$setting.Key]=$setting.Value}
if($settings['GOOS'] -cne $expectedOS -or $settings['GOARCH'] -cne $expectedArch -or $settings['vcs.revision'] -cne $commit -or $settings['vcs.modified'] -cne 'false'){throw 'Worker and Node targets or clean source identity differ.'}
$harness=Join-Path $target 'harness'
New-Item -ItemType Directory -Path $harness|Out-Null
foreach($name in @('package.json','pnpm-lock.yaml','runner.mjs','session-seams.test.mjs','README.md')){
 Copy-Item -LiteralPath (Join-Path $root ('harness/'+$name)) -Destination (Join-Path $harness $name)
}
$savedPath=$env:PATH
try{
 $env:PATH=(Split-Path $nodePath -Parent)+[IO.Path]::PathSeparator+$env:PATH
 # A portable release must not contain pnpm's absolute Windows junctions.
 $arguments=@('--dir',$harness,'install','--frozen-lockfile','--ignore-scripts','--prod','--config.node-linker=hoisted')
 if($Offline){$arguments+='--offline'}
 if($StoreDirectory){$arguments+=@('--store-dir',[IO.Path]::GetFullPath($StoreDirectory))}
 & $pnpmPath @arguments
 if($LASTEXITCODE){throw 'Pinned Harness installation failed.'}
}finally{$env:PATH=$savedPath}
$nodeDirectory=Join-Path $target 'node'
New-Item -ItemType Directory -Path $nodeDirectory|Out-Null
$nodeName=if($IsWindows){'node.exe'}else{'node'}
Copy-Item -LiteralPath $nodePath -Destination (Join-Path $nodeDirectory $nodeName)
Copy-Item -LiteralPath (Join-Path $root 'licenses/node-v24.19.0.txt') -Destination (Join-Path $nodeDirectory 'LICENSE.txt')
Copy-Item -LiteralPath (Join-Path $root 'deploy/release-contract.json') -Destination $target
Copy-Item -LiteralPath (Join-Path $root 'source-provenance.json') -Destination $target
& (Join-Path $nodeDirectory $nodeName) --test (Join-Path $harness 'session-seams.test.mjs')
if($LASTEXITCODE){throw 'Packaged Node and Harness interface checks failed.'}
. (Join-Path $PSScriptRoot 'package-files.ps1')
$links=@(Get-PortablePackageLinks $target)
# Hash all installed files. Unix relative file links are also recorded by target;
# Windows directory junctions and all absolute/escaping links are prohibited.
$files=@(Get-ChildItem -LiteralPath $target -Recurse -File | Sort-Object FullName | ForEach-Object {
 @{path=[IO.Path]::GetRelativePath($target,$_.FullName).Replace('\','/');sha256=(Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash.ToLowerInvariant()}
})
$manifest=@{format=1;role='worker';source_commit=$commit;node_version=$version;pnpm_version=$pnpmVersion;dependency_layout='hoisted-contained';links=$links;offline=[bool]$Offline;files=$files;created_at=[datetime]::UtcNow.ToString('o')}
if(@(& git -C $root status --porcelain).Count -or (& git -C $root rev-parse HEAD).Trim() -cne $commit){throw 'Source changed during packaging.'}
$manifest|ConvertTo-Json -Depth 6|Set-Content -LiteralPath (Join-Path $target 'package.json')
Write-Output ('Complete Worker package: '+$target)
