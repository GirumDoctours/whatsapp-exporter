& {
  $ErrorActionPreference = 'Stop'
  $ProgressPreference = 'SilentlyContinue'

  $base = 'https://gist.githubusercontent.com/GirumDoctours/74929d87bb6c220c23bea4d9d1b37377/raw'
  $folder = Join-Path $HOME 'WhatsApp Exporter'
  $app = Join-Path $folder 'WhatsApp Exporter.exe'

  function Say([string]$text) {
    Write-Host ''
    Write-Host "  $text"
  }

  if ([Environment]::OSVersion.Version.Major -lt 10 -or -not [Environment]::Is64BitOperatingSystem) {
    Say 'WhatsApp Exporter needs 64-bit Windows 10 or newer.'
    return
  }

  $arch = 'amd64'
  if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64' -or $env:PROCESSOR_ARCHITEW6432 -eq 'ARM64') {
    $arch = 'arm64'
  }

  New-Item -ItemType Directory -Force -Path $folder | Out-Null
  $packed = Join-Path $folder '.download.gz'
  $unpacked = Join-Path $folder '.download.exe'

  Say 'Downloading WhatsApp Exporter...'
  try {
    [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
    Invoke-WebRequest -UseBasicParsing -Uri "$base/WhatsApp-Exporter-windows-$arch.exe.gz" -OutFile $packed
    $source = [IO.File]::OpenRead($packed)
    try {
      $gzip = New-Object IO.Compression.GZipStream($source, [IO.Compression.CompressionMode]::Decompress)
      $target = [IO.File]::Create($unpacked)
      try {
        $gzip.CopyTo($target)
      } finally {
        $target.Dispose()
        $gzip.Dispose()
      }
    } finally {
      $source.Dispose()
    }
  } catch {
    Remove-Item -Force -ErrorAction SilentlyContinue $packed, $unpacked
    Say "The download didn't finish. Check your internet connection, then paste the command again."
    return
  }
  Remove-Item -Force -ErrorAction SilentlyContinue $packed

  try {
    Move-Item -Force $unpacked $app
  } catch {
    Remove-Item -Force -ErrorAction SilentlyContinue $unpacked
    if (-not (Test-Path $app)) {
      Say "Couldn't save WhatsApp Exporter in $folder."
      return
    }
    Say 'WhatsApp Exporter is already open in another window, so this one uses the copy you have.'
  }

  & $app
  if ($LASTEXITCODE -eq 0) {
    [Environment]::Exit(0)
  }
}
