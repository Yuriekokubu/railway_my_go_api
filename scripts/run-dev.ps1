$ErrorActionPreference = "Stop"

$env:APP_ENV = "development"
go run .
if ($LASTEXITCODE -ne 0) {
    exit $LASTEXITCODE
}
