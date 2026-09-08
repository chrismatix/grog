$ErrorActionPreference = 'Stop'
$env:CGO_ENABLED = '1'
$env:GOOS = 'windows'
$env:GOARCH = 'amd64'
$version = git describe --tags --always
if ($LASTEXITCODE -ne 0) { throw 'Could not determine version' }
$commit = git rev-parse --short HEAD
if ($LASTEXITCODE -ne 0) { throw 'Could not determine commit' }
$buildDate = Get-Date -Format 'yyyy-MM-ddTHH:mm:ssK'
$binaryName = 'grog-windows-amd64.exe'
New-Item -ItemType Directory -Force dist | Out-Null
go build -ldflags "-X main.version=$version -X main.commit=$commit -X main.buildDate=$buildDate -linkmode external -extldflags -static" -o "dist/$binaryName" .
if ($LASTEXITCODE -ne 0) { throw 'Windows release build failed' }
$checksum = (Get-FileHash "dist/$binaryName" -Algorithm SHA256).Hash.ToLowerInvariant()
Set-Content "dist/$binaryName.sha256" "$checksum  $binaryName`n" -NoNewline -Encoding ascii
