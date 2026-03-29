@echo off
chcp 65001 >nul 2>&1
setlocal enabledelayedexpansion

echo.
echo =========================================================
echo   LiveOps Keyboard Guard - Install (WebSocket Mode)
echo =========================================================
echo.
echo   Architecture: Guard runs as permanent background service
echo   Chrome connects via WebSocket ws://127.0.0.1:27531
echo   No Native Messaging required - zero cold-start delay
echo.

:: ── Check exe ──────────────────────────────────────────────
set "INSTALL_DIR=%~dp0"
set "INSTALL_DIR=%INSTALL_DIR:~0,-1%"
set "EXE_PATH=%INSTALL_DIR%\live-ops-keyboard.exe"

if not exist "%EXE_PATH%" (
    echo [ERROR] live-ops-keyboard.exe not found.
    echo Please make sure install.bat and live-ops-keyboard.exe are in the same folder.
    pause
    exit /b 1
)

echo [OK] Found: %EXE_PATH%
echo.

:: ── Step 1: Remove old Native Messaging registry (cleanup) ─
echo [1/2] Cleaning up old Native Messaging registry...

reg delete "HKCU\Software\Google\Chrome\NativeMessagingHosts\com.liveops.keyboard" /f >nul 2>&1
reg delete "HKLM\SOFTWARE\Google\Chrome\NativeMessagingHosts\com.liveops.keyboard" /f >nul 2>&1
echo   Old Native Messaging config removed (if existed).

:: ── Step 2: Register autostart ─────────────────────────────
echo.
echo [2/2] Registering autostart...

reg add "HKCU\Software\Microsoft\Windows\CurrentVersion\Run" ^
    /v "LiveOpsKeyboard" /t REG_SZ ^
    /d "\"%EXE_PATH%\"" /f >nul 2>&1

if %errorlevel% equ 0 (
    echo   Autostart registered: will start on every login.
) else (
    echo   [WARN] Autostart registration may have failed. Check manually.
)

:: ── Done ───────────────────────────────────────────────────
echo.
echo =========================================================
echo   Install complete!
echo.
echo   1. Reload the Chrome extension in chrome://extensions
echo   2. Guard will auto-start on every login
echo   3. Chrome connects automatically - no extra setup needed
echo =========================================================
echo.

:: Kill any old instance before starting fresh
taskkill /f /im live-ops-keyboard.exe >nul 2>&1

set /p START_NOW=  Start guard now? (Y/N): 
if /i "!START_NOW!"=="Y" (
    echo.
    echo   Starting guard...
    start "" "%EXE_PATH%"
    timeout /t 1 /nobreak >nul
    echo   Guard started. Reload Chrome extension to connect.
)

echo.
pause
