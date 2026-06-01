$ErrorActionPreference = "Stop"

New-Item -ItemType Directory -Force dist | Out-Null
go build -ldflags "-s -w -X main.appEnv=development" -o dist\my-go-api-dev.exe .
if ($LASTEXITCODE -ne 0) {
    exit $LASTEXITCODE
}

Write-Host "Built dist\my-go-api-dev.exe"
