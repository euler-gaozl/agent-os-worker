function Get-PortablePackageLinks([string]$Root){
 $links=@(Get-ChildItem -LiteralPath $Root -Recurse -Force | Where-Object { $_.Attributes -band [IO.FileAttributes]::ReparsePoint } | Sort-Object FullName)
 foreach($link in $links){
  # pnpm hoisted installs on Unix may retain relative command-file links.
  # Directory links and absolute links cannot be part of a portable package.
  if(($link.Attributes -band [IO.FileAttributes]::Directory) -or -not $link.LinkTarget -or [IO.Path]::IsPathRooted($link.LinkTarget)){throw 'Portable package contains a directory or absolute filesystem link.'}
  $resolved=$link.ResolveLinkTarget($true)
  if(-not $resolved -or -not $resolved.Exists -or -not $resolved.FullName.StartsWith($Root+[IO.Path]::DirectorySeparatorChar,[StringComparison]::Ordinal)){throw 'Package link target escapes the release or is missing.'}
  @{path=[IO.Path]::GetRelativePath($Root,$link.FullName).Replace('\','/');target=$link.LinkTarget}
 }
}
