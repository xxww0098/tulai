@echo off
setlocal
pushd "%~dp0"
set CGO_ENABLED=0
go run ./cmd/tulai-fingerprint-bundle
if errorlevel 1 (
  popd
  exit /b 1
)
set "GOFLAGS=%GOFLAGS% -tags=tulai_fingerprint_bundle"
go tool mygo build -platform windows/amd64,windows/arm64 %*
set "tulai_build_status=%errorlevel%"
popd
exit /b %tulai_build_status%
