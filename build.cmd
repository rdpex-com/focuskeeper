@echo off
rem Builds release\focuskeeper.exe and release\SHA256SUMS.txt (see build.ps1). Optional: build.cmd v1.0.0
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0build.ps1" -Version "%~1"
