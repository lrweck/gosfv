param(
    [switch]$Portable
)
$ErrorActionPreference = "Stop"
if ($Portable) {
    $env:GOEXPERIMENT = ""
} else {
    $env:GOEXPERIMENT = "simd"
}
go build -trimpath -ldflags "-s -w" -o GoSFV.exe .
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
Write-Host "Built GoSFV.exe (GOEXPERIMENT='$env:GOEXPERIMENT')"
