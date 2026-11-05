$ErrorActionPreference = 'Stop'
$root = (Get-Location).Path
$out = Join-Path $env:RUNNER_TEMP 'native-stage0-out'
$workParent = Join-Path $env:RUNNER_TEMP 'Axiom native fixture'
$runtime = Get-Content (Join-Path $out 'runtime.json') -Raw | ConvertFrom-Json
$user = 'AxiomNativeProbe'
$password = ConvertTo-SecureString ([Guid]::NewGuid().ToString() + 'aA9!') -AsPlainText -Force
$account = New-LocalUser -Name $user -Password $password -AccountNeverExpires
if (-not (Get-LocalGroupMember -SID 'S-1-5-32-545' | Where-Object { $_.SID.Value -eq $account.SID.Value })) {
    Add-LocalGroupMember -SID 'S-1-5-32-545' -Member $account
}
New-Item -ItemType Directory -Path $workParent -Force | Out-Null
$sid = $account.SID.Value
$runnerSid = [Security.Principal.WindowsIdentity]::GetCurrent().User.Value
try {
    # Only the disposable account receives access; scanning stays non-admin.
    & icacls.exe $root /grant "*${sid}:(OI)(CI)(RX)" /T /C | Out-File (Join-Path $out 'windows-acl.log')
    if ($LASTEXITCODE -ne 0) { throw 'Could not grant read access to the test payload' }
    & icacls.exe (Join-Path $env:RUNNER_TEMP 'native-stage0-runtime') /grant "*${sid}:(OI)(CI)(RX)" /T /C | Out-File (Join-Path $out 'windows-runtime-acl.log')
    if ($LASTEXITCODE -ne 0) { throw 'Could not grant read access to the test runtime' }
    # Reports contain synthetic evidence, not credential state. Keep the
    # uploader's existing access and add only the fixture user's write access.
    & icacls.exe $out /grant "*${sid}:(OI)(CI)(M)" /T /C | Out-File (Join-Path $out 'windows-private-acl.log')
    if ($LASTEXITCODE -ne 0) { throw 'Could not grant fixture evidence access' }
    # Private SDK state is separate from reports. Construct its DACL explicitly
    # and do not let the launcher's ordinary user SID inherit into children.
    $acl = [Security.AccessControl.DirectorySecurity]::new()
    $acl.SetAccessRuleProtection($true, $false)
    foreach ($identity in @($sid, 'S-1-5-18', 'S-1-5-32-544')) {
        $rule = [Security.AccessControl.FileSystemAccessRule]::new([Security.Principal.SecurityIdentifier]::new($identity), [Security.AccessControl.FileSystemRights]::FullControl, [Security.AccessControl.InheritanceFlags]'ContainerInherit,ObjectInherit', [Security.AccessControl.PropagationFlags]::None, [Security.AccessControl.AccessControlType]::Allow)
        $acl.AddAccessRule($rule)
    }
    $acl.AddAccessRule([Security.AccessControl.FileSystemAccessRule]::new([Security.Principal.SecurityIdentifier]::new($runnerSid), [Security.AccessControl.FileSystemRights]::ReadAndExecute, [Security.AccessControl.AccessControlType]::Allow))
    Set-Acl -LiteralPath $workParent -AclObject $acl
    $arguments = @(
        ('"' + (Join-Path $root 'deploy/native/probe.mjs') + '"'),
        '--out', ('"' + $out + '"'),
        '--runtime', ('"' + (Join-Path $out 'runtime.json') + '"'),
        '--work', ('"' + (Join-Path $workParent 'work') + '"')
    ) -join ' '
    $credential = [PSCredential]::new("$env:COMPUTERNAME\$user", $password)
    $process = Start-Process -FilePath $runtime.node -ArgumentList $arguments -Credential $credential -LoadUserProfile -WorkingDirectory $workParent -PassThru -RedirectStandardOutput (Join-Path $out 'probe.log') -RedirectStandardError (Join-Path $out 'probe.stderr.log')
    # Start-Process -Wait includes detached descendants and can prevent the
    # cleanup below from running. Wait for this diagnostic Node process only.
    # This deadline supervises a synthetic test, never a product execution.
    if (-not $process.WaitForExit(300000)) {
        @{status='incomplete'; reason='Disposable native diagnostic process did not exit'; fixtureOnly=$true} | ConvertTo-Json | Set-Content (Join-Path $out 'wrapper-summary.json')
        throw 'Disposable native diagnostic process did not exit; partial checkpoints were retained'
    }
    $process.Refresh()
    if ($process.ExitCode -ne 0) { throw "Non-administrator native probe exited with code $($process.ExitCode)" }
}
finally {
    # Clean only processes owned by the disposable account created above.
    $remaining = @(Get-CimInstance Win32_Process | ForEach-Object {
        $owner = Invoke-CimMethod -InputObject $_ -MethodName GetOwner -ErrorAction SilentlyContinue
        if ($owner.User -eq $user) {
            @{name=$_.Name; processId=$_.ProcessId}
            Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue
        }
    })
    @{scope='only the disposable fixture account'; remainingProcesses=$remaining} | ConvertTo-Json -Depth 5 | Set-Content (Join-Path $out 'windows-process-cleanup.json')
    Remove-LocalUser -Name $user -ErrorAction SilentlyContinue
}
