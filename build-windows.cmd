@echo off
setlocal
pushd "%~dp0"
set CGO_ENABLED=0
go tool mygo build -platform windows/amd64,windows/arm64 %*
set "tulai_build_status=%errorlevel%"
popd
exit /b %tulai_build_status%
