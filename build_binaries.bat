@echo off
REM Windows Batch Script for Cross-Compiling YALS Application
REM This script builds YALS for multiple platforms and architectures

echo Starting build process for YALS...

REM Set Go environment variables
set GO111MODULE=on
set GOPROXY=https://goproxy.io,direct
set CGO_ENABLED=0

REM Create output directories
mkdir bin 2>nul

REM Build for Windows x64
echo Building YALS for Windows x64...
set GOOS=windows
set GOARCH=amd64
go build -o bin\yals_windows_amd64.exe ./cmd/main.go
if %ERRORLEVEL% NEQ 0 (
    echo Build failed for Windows x64!
    exit /b %ERRORLEVEL%
)

REM Build for Windows ARM64
echo Building YALS for Windows ARM64...
set GOOS=windows
set GOARCH=arm64
go build -o bin\yals_windows_arm64.exe ./cmd/main.go
if %ERRORLEVEL% NEQ 0 (
    echo Build failed for Windows ARM64!
    exit /b %ERRORLEVEL%
)

REM Build for Linux x64
echo Building YALS for Linux x64...
set GOOS=linux
set GOARCH=amd64
go build -o bin\yals_linux_amd64 ./cmd/main.go
if %ERRORLEVEL% NEQ 0 (
    echo Build failed for Linux x64!
    exit /b %ERRORLEVEL%
)

REM Build for Linux ARM64
echo Building YALS for Linux ARM64...
set GOOS=linux
set GOARCH=arm64
go build -o bin\yals_linux_arm64 ./cmd/main.go
if %ERRORLEVEL% NEQ 0 (
    echo Build failed for Linux ARM64!
    exit /b %ERRORLEVEL%
)

REM Build for macOS x64
echo Building YALS for macOS x64...
set GOOS=darwin
set GOARCH=amd64
go build -o bin\yals_darwin_amd64 ./cmd/main.go
if %ERRORLEVEL% NEQ 0 (
    echo Build failed for macOS x64!
    exit /b %ERRORLEVEL%
)

REM Build for macOS ARM64 (Apple Silicon)
echo Building YALS for macOS ARM64...
set GOOS=darwin
set GOARCH=arm64
go build -o bin\yals_darwin_arm64 ./cmd/main.go
if %ERRORLEVEL% NEQ 0 (
    echo Build failed for macOS ARM64!
    exit /b %ERRORLEVEL%
)

echo.
echo Build completed successfully!
echo Output files in bin directory:
dir /b bin\yals_*