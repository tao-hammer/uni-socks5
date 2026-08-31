$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

function Find-Go {
    $cmd = Get-Command go -ErrorAction SilentlyContinue
    if ($cmd) { return $cmd.Source }
    $candidates = @(
        "$env:GOROOT\bin\go.exe",
        "C:\Program Files\Go\bin\go.exe",
        "$env:LOCALAPPDATA\Programs\Go\bin\go.exe"
    )
    foreach ($p in $candidates) {
        if ($p -and (Test-Path $p)) { return $p }
    }
    return $null
}

$go = Find-Go
if (-not $go) {
    $tools = Join-Path $root "dist\.tools"
    New-Item -ItemType Directory -Force -Path $tools | Out-Null
    $goRoot = Join-Path $tools "go"
    $goExe = Join-Path $goRoot "bin\go.exe"
    if (-not (Test-Path $goExe)) {
        $zip = Join-Path $tools "go.zip"
        $url = "https://dl.google.com/go/go1.22.12.windows-amd64.zip"
        Write-Host "Go not found, downloading $url"
        Invoke-WebRequest -Uri $url -OutFile $zip
        if (Test-Path $goRoot) { Remove-Item -Recurse -Force $goRoot }
        Expand-Archive -Path $zip -DestinationPath $tools -Force
    }
    $env:GOROOT = $goRoot
    $env:PATH = "$(Join-Path $goRoot 'bin');$env:PATH"
    $go = $goExe
}

Write-Host "Using Go: $go"
& $go version
$env:GOPROXY = "https://goproxy.cn,direct"
$env:GOSUMDB = "sum.golang.google.cn"
& $go mod tidy
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
& $go run .\cmd\mkipk
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
