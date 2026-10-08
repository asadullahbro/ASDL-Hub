# ASDL Hub command-line installer for Windows.
#
#   irm https://get.asdl.website/asdl-hub | iex
#
# Installs the asdl-hub command so you can manage a Hub from this machine. The
# Hub itself (the server) runs on Linux only, so it is not offered here.
#
# Settings (environment variables):
#   ASDL_BIN_DIR      where to put it (default: %LOCALAPPDATA%\Programs\asdl-hub)
#   ASDL_CLI_VERSION  a release to install, such as v0.14.0 (default: latest)

$AsdlVersion = ''   # stamped with the release tag when the release is published

function Install-AsdlHubCli {
    $ErrorActionPreference = 'Stop'
    $ProgressPreference = 'SilentlyContinue'   # the progress bar makes downloads very slow in Windows PowerShell
    [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
    $repo = 'asadullahbro/ASDL-Hub'
    $docs = 'https://docs.asdl.website/hub/cli/'

    Write-Host ''
    Write-Host '  1) ASDL Hub        Linux only, so not available on Windows'
    Write-Host '  2) Command line    manage a Hub from this machine'
    Write-Host ''
    Write-Host '-> Installing the command line.'

    try {
        $archName = if ($env:PROCESSOR_ARCHITEW6432) { $env:PROCESSOR_ARCHITEW6432 } else { $env:PROCESSOR_ARCHITECTURE }
        switch ($archName) {
            'AMD64' { $arch = 'amd64' }
            'ARM64' { $arch = 'arm64' }
            default { throw "Unsupported architecture: $archName." }
        }

        $version = if ($env:ASDL_CLI_VERSION) { $env:ASDL_CLI_VERSION } else { $AsdlVersion }
        if (-not $version -or $version -eq 'latest') {
            $version = (Invoke-RestMethod -UseBasicParsing "https://api.github.com/repos/$repo/releases/latest").tag_name
        }
        if ($version -notmatch '^v\d+\.\d+\.\d+([.-][0-9A-Za-z.-]+)?$') {
            throw "Couldn't work out which release to install (got '$version')."
        }

        $dir = if ($env:ASDL_BIN_DIR) { $env:ASDL_BIN_DIR } else { Join-Path $env:LOCALAPPDATA 'Programs\asdl-hub' }
        $name = "asdl-hub-windows-$arch.exe"
        $base = "https://github.com/$repo/releases/download/$version"
        $tmp = Join-Path ([IO.Path]::GetTempPath()) ("asdl-hub-" + [guid]::NewGuid().ToString('N'))
        New-Item -ItemType Directory -Path $tmp | Out-Null

        try {
            Write-Host "-> Installing asdl-hub $version (windows-$arch) to $dir"
            try {
                Invoke-WebRequest -UseBasicParsing "$base/SHA256SUMS" -OutFile (Join-Path $tmp 'SHA256SUMS')
            } catch {
                throw "Release $version wasn't found."
            }
            $line = Get-Content (Join-Path $tmp 'SHA256SUMS') | Where-Object { ($_ -split '\s+')[1] -eq $name } | Select-Object -First 1
            if (-not $line) { throw "Release $version has no command-line build for windows-$arch." }
            $want = ($line -split '\s+')[0].ToLower()

            $download = Join-Path $tmp $name
            Invoke-WebRequest -UseBasicParsing "$base/$name" -OutFile $download
            $got = (Get-FileHash -Algorithm SHA256 $download).Hash.ToLower()
            if ($got -ne $want) { throw "Checksum mismatch for $name; not installing." }
            if (-not (& $download version 2>$null)) { throw "The downloaded program doesn't run on this machine." }

            New-Item -ItemType Directory -Force -Path $dir | Out-Null
            $target = Join-Path $dir 'asdl-hub.exe'
            # A running copy can be renamed but not overwritten.
            if (Test-Path $target) { Move-Item -Force $target "$target.old" }
            Copy-Item -Force $download $target
            Remove-Item -Force "$target.old" -ErrorAction SilentlyContinue

            Write-Host "OK Installed $(& $target version) at $target"

            $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
            if (-not (($userPath -split ';') -contains $dir)) {
                [Environment]::SetEnvironmentVariable('Path', ($(if ($userPath) { "$userPath;" } else { '' }) + $dir), 'User')
                $env:Path = "$env:Path;$dir"
                Write-Host "   Added $dir to your PATH. Open a new terminal window to use asdl-hub there."
            }
            Write-Host ''
            Write-Host 'Next: asdl-hub login https://your-hub.example.com'
        } finally {
            Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
        }
    } catch {
        Write-Host "X $($_.Exception.Message)" -ForegroundColor Red
        Write-Host "  See $docs"
    }
}

Install-AsdlHubCli
