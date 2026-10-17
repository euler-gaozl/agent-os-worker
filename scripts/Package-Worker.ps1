param(
 [Parameter(Mandatory)][string]$Destination,
 [string]$Node='node',
 [string]$Pnpm='pnpm',
 [string]$StoreDirectory='',
	[string]$SourceArtifact='',
 [string]$ResidentArtifact='',
 [string]$BudgetArtifact='',
 [string]$NativeArtifact='',
 [string]$FinalArtifact='',
 [string]$GuardedArtifact='',
 [string]$GuardedV6Artifact='',
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
$sourceEngines=@()
foreach($selection in @(
 @{artifact=$SourceArtifact;lock='source-harness-lock.json';directory='source-harness';engine='dsh-source-639ed015-state-v1'},
 @{artifact=$ResidentArtifact;lock='resident-harness-lock.json';directory='resident-harness';engine='dsh-source-639ed015-resident-v2'},
 @{artifact=$BudgetArtifact;lock='budget-harness-lock.json';directory='budget-harness';engine='dsh-source-639ed015-budget-v3'},
 @{artifact=$NativeArtifact;lock='native-harness-lock.json';directory='native-harness';engine='dsh-source-639ed015-native-v4'},
 @{artifact=$FinalArtifact;lock='final-harness-lock.json';directory='final-harness';engine='dsh-source-639ed015-final-v5'},
 @{artifact=$GuardedArtifact;lock='guarded-harness-lock.json';directory='guarded-harness';engine='dsh-source-639ed015-guarded-v7'},
 @{artifact=$GuardedV6Artifact;lock='guarded-v6-harness-lock.json';directory='guarded-v6-harness';engine='dsh-source-639ed015-guarded-v6'}
)){
 if(-not $selection.artifact){continue}
 $artifact=(Resolve-Path -LiteralPath $selection.artifact).Path
 $pin=Get-Content -LiteralPath (Join-Path $root ('worker/'+$selection.lock)) -Raw|ConvertFrom-Json
 if($pin.engine -cne $selection.engine){throw 'Compiled source pin names the wrong version.'}
 $manifestFile=Join-Path $artifact 'dist/manifest.json'
 if((Get-FileHash -LiteralPath $manifestFile).Hash.ToLowerInvariant() -cne $pin.manifest_sha256){throw 'Source artifact does not match the compiled Worker pin.'}
 $sourceManifest=Get-Content -LiteralPath $manifestFile -Raw|ConvertFrom-Json
 if($sourceManifest.source_engine -cne $pin.engine -or $sourceManifest.upstream_commit -cne $pin.upstream_commit){throw 'Source engine provenance mismatch.'}
 $sourceDestination=Join-Path $target $selection.directory
 New-Item -ItemType Directory -Path (Join-Path $sourceDestination 'dist')|Out-Null
 $sourceFiles=@{'source-runner.mjs'=$sourceManifest.source_runner_sha256;'dist/kernel.mjs'=$sourceManifest.kernel_sha256;'dist/THIRD_PARTY_NOTICES.txt'=$sourceManifest.notices_sha256;'dist/manifest.json'=$pin.manifest_sha256}
 foreach($relative in $sourceFiles.Keys){
  $inputFile=Get-Item -LiteralPath (Join-Path $artifact $relative)
  if(($inputFile.Attributes -band [IO.FileAttributes]::ReparsePoint) -or $inputFile.PSIsContainer){throw 'Source package files must be regular files.'}
  $outputFile=Join-Path $sourceDestination $relative
  Copy-Item -LiteralPath $inputFile.FullName -Destination $outputFile
  if((Get-FileHash -LiteralPath $outputFile).Hash.ToLowerInvariant() -cne $sourceFiles[$relative]){throw 'Copied source artifact digest mismatch.'}
 }
 Copy-Item -LiteralPath (Join-Path $root ('worker/'+$selection.lock)) -Destination (Join-Path $sourceDestination 'source-lock.json')
 $sourceEngines+=$pin.engine
}
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
if($SourceArtifact){$manifest.source_engine='dsh-source-639ed015-state-v1';$manifest.source_activation='Opt-in DSH_SOURCE_RUNNER=<package>/source-harness/source-runner.mjs; compiled digest verified before claiming.'}
if($ResidentArtifact){$manifest.resident_engine='dsh-source-639ed015-resident-v2';$manifest.resident_activation='Opt-in DSH_RESIDENT_RUNNER=<package>/resident-harness/source-runner.mjs; requires Runtime with explicit resident-v2 support.'}
if($BudgetArtifact){$manifest.budget_engine='dsh-source-639ed015-budget-v3';$manifest.budget_activation='Opt-in DSH_BUDGET_RUNNER=<package>/budget-harness/source-runner.mjs; requires explicit release/rollback support for durable model receipt protocol 1.'}
if($GuardedArtifact){$manifest.guarded_engine='dsh-source-639ed015-guarded-v7';$manifest.guarded_activation='Explicit DSH_GUARDED_RUNNER, protocol 1 input validation and matching Runtime/release gates required.'}
if($GuardedV6Artifact){$manifest.guarded_v6_engine='dsh-source-639ed015-guarded-v6';$manifest.guarded_v6_activation='Explicit DSH_GUARDED_V6_RUNNER for resumable v6 history; terminal history may run without it.'}
$manifest.source_engines=$sourceEngines
if($NativeArtifact){$manifest.native_engine='dsh-source-639ed015-native-v4';$manifest.native_activation='Opt-in DSH_NATIVE_RUNNER=<package>/native-harness/source-runner.mjs; requires native model receipt protocol 2 and compatible managed release gates.'}
if($FinalArtifact){$manifest.final_engine='dsh-source-639ed015-final-v5';$manifest.final_activation='Opt-in DSH_FINAL_RUNNER=<package>/final-harness/source-runner.mjs; requires frozen final_output policy, finalization protocol 1 and compatible release/rollback gates.'}
if(@(& git -C $root status --porcelain).Count -or (& git -C $root rev-parse HEAD).Trim() -cne $commit){throw 'Source changed during packaging.'}
$manifest|ConvertTo-Json -Depth 6|Set-Content -LiteralPath (Join-Path $target 'package.json')
Write-Output ('Complete Worker package: '+$target)
