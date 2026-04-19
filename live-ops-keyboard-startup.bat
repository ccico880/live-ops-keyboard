@echo off
::: live-ops-keyboard-startup.bat
::: 静默启动脚本 - 用于开机自启，不弹任何窗口
::: 注意：bat 放在 exe 同目录，%~dp0 自动获取所在位置

setlocal

::: 获取脚本所在目录（通用，自动适配任何路径）
set "SCRIPT_DIR=%~dp0"
set "SCRIPT_DIR=%SCRIPT_DIR:~0,-1%"

::: exe 路径
set "EXE_PATH=%SCRIPT_DIR%\live-ops-keyboard.exe"

::: 检查 exe 是否存在
if not exist "%EXE_PATH%" (
    exit /b 1
)

::: VBS 与 bat 同目录，直接调用
cscript //nologo "%SCRIPT_DIR%\run_kb.vbs"

exit /b 0
