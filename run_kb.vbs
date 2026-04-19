Set o = CreateObject("WScript.Shell")
o.Run Chr(34) & Replace(WScript.ScriptFullName, "run_kb.vbs", "live-ops-keyboard.exe") & Chr(34), 0, False
