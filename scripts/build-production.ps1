$ErrorActionPreference = "Stop"

New-Item -ItemType Directory -Force dist | Out-Null
go build -ldflags "-s -w -X main.appEnv=production" -o dist\my-go-api.exe .
if ($LASTEXITCODE -ne 0) {
    exit $LASTEXITCODE
}

Write-Host "Built dist\my-go-api.exe"
