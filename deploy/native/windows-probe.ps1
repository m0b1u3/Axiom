$ErrorActionPreference = 'Stop'
$root = (Get-Location).Path
$out = Join-Path $env:RUNNER_TEMP 'native-stage0-out'
$workParent = Join-Path $env:RUNNER_TEMP 'Axiom native fixture'
$runtime = Get-Content (Join-Path $out 'runtime.json') -Raw | ConvertFrom-Json
$user = 'AxiomNativeProbe'
$password = ConvertTo-SecureString ([Guid]::NewGuid().ToString() + 'aA9!') -AsPlainText -Force
$account = New-LocalUser -Name $user -Password $password -AccountNeverExpires
New-Item -ItemType Directory -Path $workParent -Force | Out-Null
$sid = $account.SID.Value
$runnerSid = [Security.Principal.WindowsIdentity]::GetCurrent().User.Value
try {
    # Only the disposable account receives access; scanning stays non-admin.
    & icacls.exe $root /grant "*${sid}:(OI)(CI)(RX)" /T /C | Out-File (Join-Path $out 'windows-acl.log')
    if ($LASTEXITCODE -ne 0) { throw 'Could not grant read access to the test payload' }
    & icacls.exe (Join-Path $env:RUNNER_TEMP 'native-stage0-runtime') /grant "*${sid}:(OI)(CI)(RX)" /T /C | Out-File (Join-Path $out 'windows-runtime-acl.log')
    if ($LASTEXITCODE -ne 0) { throw 'Could not grant read access to the test runtime' }
    # The artifact uploader may have a filtered administrator token. Preserve
    # its actual user SID explicitly, rather than relying on Administrators.
    & icacls.exe $out /inheritance:r /grant:r "*${sid}:(OI)(CI)(M)" "*${runnerSid}:(OI)(CI)(F)" '*S-1-5-18:(OI)(CI)(F)' '*S-1-5-32-544:(OI)(CI)(F)' /T /C | Out-File (Join-Path $out 'windows-private-acl.log')
    if ($LASTEXITCODE -ne 0) { throw 'Could not protect evidence directory' }
    # The launcher can access this parent, while new private credential
    # directories do not inherit a second user's access entry.
    & icacls.exe $workParent /inheritance:r /grant:r "*${sid}:(OI)(CI)(M)" "*${runnerSid}:(F)" '*S-1-5-18:(OI)(CI)(F)' '*S-1-5-32-544:(OI)(CI)(F)' | Out-File (Join-Path $out 'windows-private-acl.log') -Append
    if ($LASTEXITCODE -ne 0) { throw 'Could not protect fixture directory' }
    $arguments = @(
        ('"' + (Join-Path $root 'deploy/native/probe.mjs') + '"'),
        '--out', ('"' + $out + '"'),
        '--runtime', ('"' + (Join-Path $out 'runtime.json') + '"'),
        '--work', ('"' + (Join-Path $workParent 'work') + '"')
    ) -join ' '
    $credential = [PSCredential]::new("$env:COMPUTERNAME\$user", $password)
    $process = Start-Process -FilePath $runtime.node -ArgumentList $arguments -Credential $credential -LoadUserProfile -WorkingDirectory $workParent -PassThru -Wait -RedirectStandardOutput (Join-Path $out 'probe.log') -RedirectStandardError (Join-Path $out 'probe.stderr.log')
    if ($process.ExitCode -ne 0) { throw "Non-administrator native probe exited with code $($process.ExitCode)" }
}
finally {
    # Clean only processes owned by the disposable account created above.
    Get-CimInstance Win32_Process | ForEach-Object {
        $owner = Invoke-CimMethod -InputObject $_ -MethodName GetOwner -ErrorAction SilentlyContinue
        if ($owner.User -eq $user) { Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue }
    }
    Remove-LocalUser -Name $user -ErrorAction SilentlyContinue
}
