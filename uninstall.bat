@echo off
echo  Uninstalling LiveOps Keyboard Guard...

:: Stop process
taskkill /f /im live-ops-keyboard.exe >nul 2>&1

:: Remove registry keys
reg delete "HKCU\Software\Google\Chrome\NativeMessagingHosts\com.liveops.keyboard" /f >nul 2>&1
reg delete "HKCU\Software\Microsoft\Windows\CurrentVersion\Run" /v "LiveOpsKeyboard" /f >nul 2>&1

echo  Uninstall complete.
pause
