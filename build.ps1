param(
    [switch]$Simd
)
$ErrorActionPreference = "Stop"
if ($Simd) {
    $env:GOEXPERIMENT = "simd"
} else {
    $env:GOEXPERIMENT = ""
}
go build -trimpath -ldflags "-s -w" -o GoSFV.exe .
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
Write-Host "Built GoSFV.exe (GOEXPERIMENT='$env:GOEXPERIMENT')"
