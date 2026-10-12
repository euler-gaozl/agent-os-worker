param([string]$Destination='bin')
$ErrorActionPreference='Stop'
$root=[IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$destinationPath=if([IO.Path]::IsPathRooted($Destination)){$Destination}else{Join-Path $root $Destination}
New-Item -ItemType Directory -Path $destinationPath -Force | Out-Null
$saved=@{};foreach($key in @('GOWORK','GOPROXY','GOENV','GOFLAGS','GOTOOLCHAIN','GOROOT','GOOS','GOARCH','GOAMD64','GOEXPERIMENT','CGO_ENABLED')){$saved[$key]=[Environment]::GetEnvironmentVariable($key)}
try {
 $env:GOWORK='off';$env:GOPROXY='off'
 $env:GOENV='off';$env:GOFLAGS='';$env:GOTOOLCHAIN='local';$env:GOROOT=$null
 $env:GOOS=$null;$env:GOARCH=$null;$env:GOAMD64='v1';$env:GOEXPERIMENT='';$env:CGO_ENABLED='0'
 $targetOS=(& go env GOHOSTOS).Trim();if($LASTEXITCODE){throw 'Cannot identify Go host OS.'}
 $targetArch=(& go env GOHOSTARCH).Trim();if($LASTEXITCODE){throw 'Cannot identify Go host architecture.'}
 $env:GOOS=$targetOS;$env:GOARCH=$targetArch
 Push-Location $root
 try {
  $suffix=if($targetOS -eq 'windows'){'.exe'}else{''}
  foreach($command in @('worker','healthcheck')) {
   & go build -mod=vendor -trimpath -ldflags='-s -w' -o (Join-Path $destinationPath ($command+$suffix)) ("./cmd/"+$command)
   if($LASTEXITCODE){throw "Build failed: $command"}
   $info=(& go version -m -json (Join-Path $destinationPath ($command+$suffix)))|ConvertFrom-Json
   if($LASTEXITCODE){throw 'Cannot verify executable build identity.'}
   $settings=@{};foreach($setting in $info.Settings){$settings[$setting.Key]=$setting.Value}
   if($info.Main.Path -cne 'github.com/euler-gaozl/agent-os-worker' -or $settings['GOOS'] -cne $targetOS -or $settings['GOARCH'] -cne $targetArch){throw 'Compiled executable is not the expected native Worker module.'}
  }
  $hashes=Get-ChildItem -LiteralPath $destinationPath -File | Where-Object Name -ne 'sha256.json' | ForEach-Object {@{name=$_.Name;sha256=(Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash.ToLowerInvariant()}}
  [IO.File]::WriteAllText((Join-Path $destinationPath 'sha256.json'),(ConvertTo-Json -InputObject @($hashes)))
 }finally{Pop-Location}
}finally{foreach($key in $saved.Keys){[Environment]::SetEnvironmentVariable($key,$saved[$key])}}
