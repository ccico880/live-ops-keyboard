@echo off
:: live-ops-keyboard-startup.bat
:: 静默启动脚本 - 用于开机自启，不弹任何窗口
:: 放到与 exe 同目录

setlocal

:: 获取脚本所在目录
set "SCRIPT_DIR=%~dp0"
set "SCRIPT_DIR=%SCRIPT_DIR:~0,-1%"

:: exe 路径
set "EXE_PATH=%SCRIPT_DIR%\live-ops-keyboard.exe"

:: 检查 exe 是否存在
if not exist "%EXE_PATH%" (
    exit /b 1
)

:: 用 vbs 脚本隐藏启动（最彻底，不弹任何窗口）
echo.Set o=CreateObject("WScript.Shell")> "%TEMP%\run_kb.vbs"
echo.o.Run """%EXE_PATH%""", 0, False>> "%TEMP%\run_kb.vbs"
cscript //nologo "%TEMP%\run_kb.vbs"
del "%TEMP%\run_kb.vbs"

exit /b 0
