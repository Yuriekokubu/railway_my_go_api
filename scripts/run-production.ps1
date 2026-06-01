$ErrorActionPreference = "Stop"

$env:APP_ENV = "production"
.\dist\my-go-api.exe
if ($LASTEXITCODE -ne 0) {
    exit $LASTEXITCODE
}
