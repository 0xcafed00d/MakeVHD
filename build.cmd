@echo off
setlocal

set "REPO_ROOT=%~dp0"
set "DIST_DIR=%REPO_ROOT%dist"

if not exist "%DIST_DIR%" mkdir "%DIST_DIR%"

pushd "%REPO_ROOT%" || exit /b 1

call :build_target makevhd-linux-amd64 linux amd64 "" || goto :fail
call :build_target makevhd-linux-armv7 linux arm 7 || goto :fail
call :build_target makevhd-linux-arm64 linux arm64 "" || goto :fail
call :build_target makevhd-windows-amd64.exe windows amd64 "" || goto :fail

copy /Y "mount-image.sh" "%DIST_DIR%\" >nul || goto :fail
copy /Y "mount-image.ps1" "%DIST_DIR%\" >nul || goto :fail

echo Build completed. Artifacts are in "%DIST_DIR%".
popd
exit /b 0

:build_target
set "NAME=%~1"
set "TARGET_GOOS=%~2"
set "TARGET_GOARCH=%~3"
set "TARGET_GOARM=%~4"

echo building %NAME%

if defined TARGET_GOARM (
    set "GOARM=%TARGET_GOARM%"
) else (
    set "GOARM="
)

set "CGO_ENABLED=0"
set "GOOS=%TARGET_GOOS%"
set "GOARCH=%TARGET_GOARCH%"
go build -trimpath -o "%DIST_DIR%\%NAME%" .
if errorlevel 1 exit /b 1

set "CGO_ENABLED="
set "GOOS="
set "GOARCH="
set "GOARM="
exit /b 0

:fail
popd
exit /b 1
