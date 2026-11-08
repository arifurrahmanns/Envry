# Install Envry on Windows. In PowerShell, run:
#
#   irm https://raw.githubusercontent.com/arifurrahmanns/envry/main/install.ps1 | iex
#
# It downloads the latest release for your processor, verifies it against the
# release's checksums.txt, installs envry.exe to %LOCALAPPDATA%\Programs\Envry,
# adds that folder to your user PATH and adds Envry to the Start menu.
# No administrator rights are needed. Run it again to upgrade.
#
# Environment variables:
#   ENVRY_VERSION         release tag to install, e.g. v0.1.0 (default: the latest release)
#   ENVRY_INSTALL_DIR     folder for envry.exe (default: %LOCALAPPDATA%\Programs\Envry)
#   ENVRY_NO_MODIFY_PATH  set to 1 to leave PATH and the Start menu untouched

& {
    $ErrorActionPreference = 'Stop'
    # The progress bar makes downloads very slow in Windows PowerShell 5.1.
    $ProgressPreference = 'SilentlyContinue'
    [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

    $repo = 'arifurrahmanns/envry'

    function Test-SameFolder([string]$a, [string]$b) {
        return $a.TrimEnd('\') -ieq $b.TrimEnd('\')
    }

    # Adds $dir to the user PATH in the registry, keeping %VARIABLE% entries
    # unexpanded, then tells Windows so newly started programs see it.
    function Add-UserPath([string]$dir) {
        $key = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment', $true)
        try {
            $current = $key.GetValue('Path', '', [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
            $entries = @($current -split ';' | Where-Object { $_ })
            if ($entries | Where-Object { Test-SameFolder $_ $dir }) {
                return $false
            }
            $key.SetValue('Path', (($entries + $dir) -join ';'), [Microsoft.Win32.RegistryValueKind]::ExpandString)
        }
        finally {
            $key.Close()
        }
        if (-not ('EnvryInstall.Native' -as [type])) {
            Add-Type -Namespace EnvryInstall -Name Native -MemberDefinition @'
[DllImport("user32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
public static extern IntPtr SendMessageTimeout(IntPtr hWnd, uint msg, UIntPtr wParam, string lParam, uint flags, uint timeout, out UIntPtr result);
'@
        }
        $result = [UIntPtr]::Zero
        # HWND_BROADCAST, WM_SETTINGCHANGE, SMTO_ABORTIFHUNG
        [void][EnvryInstall.Native]::SendMessageTimeout([IntPtr]0xffff, 0x1A, [UIntPtr]::Zero, 'Environment', 2, 5000, [ref]$result)
        return $true
    }

    try {
        $arch = $env:PROCESSOR_ARCHITEW6432
        if (-not $arch) { $arch = $env:PROCESSOR_ARCHITECTURE }
        switch ($arch) {
            'AMD64' { $arch = 'amd64' }
            'ARM64' { $arch = 'arm64' }
            default { throw "Envry has no build for the $arch processor architecture." }
        }

        if ($env:ENVRY_VERSION) {
            $base = "https://github.com/$repo/releases/download/$($env:ENVRY_VERSION)"
        }
        else {
            $base = "https://github.com/$repo/releases/latest/download"
        }
        if ($env:ENVRY_INSTALL_DIR) {
            $dir = $env:ENVRY_INSTALL_DIR
        }
        else {
            $dir = Join-Path $env:LOCALAPPDATA 'Programs\Envry'
        }
        $archive = "envry-windows-$arch.zip"
        $exe = Join-Path $dir 'envry.exe'

        $tmp = Join-Path ([IO.Path]::GetTempPath()) ('envry-install-' + [Guid]::NewGuid().ToString('N'))
        New-Item -ItemType Directory -Path $tmp | Out-Null
        try {
            Write-Host "Downloading $archive"
            foreach ($name in @($archive, 'checksums.txt')) {
                try {
                    Invoke-WebRequest -UseBasicParsing -Uri "$base/$name" -OutFile (Join-Path $tmp $name)
                }
                catch {
                    throw "Could not download $base/$name ($($_.Exception.Message)). Check your connection, and that ENVRY_VERSION names an existing release."
                }
            }

            $expected = $null
            foreach ($line in Get-Content (Join-Path $tmp 'checksums.txt')) {
                $parts = $line.Trim() -split '\s+'
                if ($parts.Count -ge 2 -and $parts[1].TrimStart('*') -eq $archive) {
                    $expected = $parts[0].ToLowerInvariant()
                }
            }
            if (-not $expected) { throw "$archive is not listed in checksums.txt." }
            $actual = (Get-FileHash -Algorithm SHA256 -Path (Join-Path $tmp $archive)).Hash.ToLowerInvariant()
            if ($actual -ne $expected) { throw "Checksum mismatch for $archive; the download is corrupt or was tampered with." }

            Expand-Archive -Path (Join-Path $tmp $archive) -DestinationPath (Join-Path $tmp 'files') -Force
            New-Item -ItemType Directory -Path $dir -Force | Out-Null
            try {
                Copy-Item -Path (Join-Path $tmp 'files\envry.exe') -Destination $exe -Force
            }
            catch {
                throw "Could not write $exe. If Envry is running, close it and run the installer again."
            }
        }
        finally {
            Remove-Item -Recurse -Force -Path $tmp -ErrorAction SilentlyContinue
        }

        $installed = (& $exe version | Select-Object -First 1)
        Write-Host "Installed $installed"
        Write-Host "  to $exe"

        if ($env:ENVRY_NO_MODIFY_PATH -eq '1') {
            return
        }

        if (Add-UserPath $dir) {
            Write-Host "Added $dir to your PATH."
        }
        if (-not (($env:Path -split ';') | Where-Object { $_ -and (Test-SameFolder $_ $dir) })) {
            $env:Path = "$env:Path;$dir"
        }

        # The shortcut opens Envry in its own window and the web UI in the browser.
        # "Envry" is the desktop app's shortcut; older versions of this script
        # used that name for the console one, so move it if it is ours.
        $programs = [Environment]::GetFolderPath('Programs')
        $shell = New-Object -ComObject WScript.Shell
        $old = Join-Path $programs 'Envry.lnk'
        if ((Test-Path -LiteralPath $old) -and (Test-SameFolder $shell.CreateShortcut($old).TargetPath $exe)) {
            Remove-Item -LiteralPath $old -Force
        }
        $shortcut = Join-Path $programs 'Envry (console).lnk'
        $link = $shell.CreateShortcut($shortcut)
        $link.TargetPath = $exe
        $link.WorkingDirectory = $dir
        $link.Description = 'Envry local email testing server'
        $link.Save()
        Write-Host 'Added "Envry (console)" to the Start menu.'

        Write-Host ''
        Write-Host 'Start Envry from the Start menu, or run "envry" in a terminal.'
    }
    catch {
        Write-Host "envry install: $($_.Exception.Message)" -ForegroundColor Red
    }
}
