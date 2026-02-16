@echo off
title AutoBot - Mercari Deal Hunter
color 0A
echo.
echo  ====================================
echo   AutoBot - Mercari Deal Hunter
echo   Scanning for Designer Brand Deals
echo  ====================================
echo.

cd /d "%~dp0"

REM Check if Go is installed
where go >nul 2>&1
if %errorlevel% neq 0 (
    echo [ERROR] Go is not installed!
    echo.
    echo Please install Go from: https://go.dev/dl/
    echo Or run the pre-built binary: autobot.exe
    echo.
    if exist autobot.exe (
        echo Found autobot.exe, launching...
        autobot.exe
    ) else (
        echo No binary found. Please build first:
        echo   go build -o autobot.exe ./cmd/autobot/
        pause
    )
    exit /b
)

echo [INFO] Building AutoBot...
go build -o autobot.exe ./cmd/autobot/
if %errorlevel% neq 0 (
    echo [ERROR] Build failed!
    pause
    exit /b
)

echo [INFO] Starting AutoBot...
echo.
autobot.exe
pause
