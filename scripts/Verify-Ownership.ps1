$ErrorActionPreference='Stop'
$root=[IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
if(Test-Path (Join-Path $root 'cmd/migrate')){throw 'Worker must not own a migration command'}
$expected=@('github.com/euler-gaozl/agent-os-runtime/execution','github.com/euler-gaozl/agent-os-runtime/internal/runstate')
$deps=@(& go -C $root list -mod=vendor -deps ./cmd/worker)
if($LASTEXITCODE){throw 'Dependency inspection failed'}
$owner=@($deps|Where-Object {$_.StartsWith('github.com/euler-gaozl/agent-os-runtime/')})
if(@(Compare-Object ($expected|Sort-Object) ($owner|Sort-Object)).Count){throw 'Worker depends on Runtime API, effects or migration package'}
if(Test-Path (Join-Path $root 'vendor/github.com/euler-gaozl/agent-os-runtime/runtime')){throw 'Worker must not vendor Runtime server/migrations'}
Write-Output 'Verified narrow execution dependency closure and Runtime migration ownership.'
