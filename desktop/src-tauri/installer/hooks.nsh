; Stop Lucidbench processes that run from the install directory, so a silent
; update or uninstall is not blocked by a locked lucidd.exe, and so the new app
; never attaches to a stale engine. Instances running from elsewhere (a dev
; build, another install) are left alone: the image path must be under $INSTDIR.

!macro LucidStopInstalled
  nsExec::Exec `powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -Command "$$d = '$INSTDIR\'; Get-Process -Name 'lucidbench-desktop','lucidd' -ErrorAction SilentlyContinue | Where-Object { $$_.Path -and $$_.Path.StartsWith($$d, [System.StringComparison]::OrdinalIgnoreCase) } | Stop-Process -Force -ErrorAction SilentlyContinue; Start-Sleep -Milliseconds 800"`
  Pop $0
!macroend

!macro NSIS_HOOK_PREINSTALL
  !insertmacro LucidStopInstalled
!macroend

!macro NSIS_HOOK_PREUNINSTALL
  !insertmacro LucidStopInstalled
!macroend
