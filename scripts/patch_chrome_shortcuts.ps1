# restore_chrome.ps1 - 恢复 Chrome 快捷方式，去掉 --remote-debugging-port
$shell = New-Object -ComObject WScript.Shell

$locations = @(
    [Environment]::GetFolderPath('Desktop'),
    [Environment]::GetFolderPath('StartMenu'),
    [Environment]::GetFolderPath('CommonDesktopDirectory'),
    [Environment]::GetFolderPath('CommonStartMenu'),
    "$env:APPDATA\Microsoft\Internet Explorer\Quick Launch\User Pinned\TaskBar",
    "$env:APPDATA\Microsoft\Internet Explorer\Quick Launch",
    "$env:PUBLIC\Desktop"
)

$modified = 0

foreach ($loc in $locations) {
    if (!(Test-Path $loc)) { continue }
    Get-ChildItem -Path $loc -Filter '*.lnk' -Recurse -ErrorAction SilentlyContinue | ForEach-Object {
        try {
            $lnk = $shell.CreateShortcut($_.FullName)
            $tgt = $lnk.TargetPath
            if ($tgt -match 'chrome\.exe$' -or $tgt -match 'Google\\Chrome\\Application') {
                if ($lnk.Arguments -match 'remote-debugging-port') {
                    $oldArgs = $lnk.Arguments
                    $newArgs = $oldArgs -replace '\s*--remote-debugging-port=\d+\s*', ' '
                    $newArgs = $newArgs.Trim()
                    $lnk.Arguments = $newArgs
                    $lnk.Save()
                    $modified++
                    Write-Host "Restored: $($_.FullName)" -ForegroundColor Green
                    Write-Host "  '$oldArgs' -> '$newArgs'"
                }
            }
        } catch {
            # Skip files we can't write
        }
    }
}

Write-Host "`nRestored $modified shortcuts"
