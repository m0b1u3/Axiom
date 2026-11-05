$ErrorActionPreference = 'Stop'
$root = (Get-Location).Path
$out = Join-Path $env:RUNNER_TEMP 'native-stage0-out'
$volumeRoot = [IO.Path]::GetPathRoot($env:ProgramFiles)
$workParent = Join-Path $volumeRoot ('AxiomNativeStage0-' + [Guid]::NewGuid().ToString('N'))
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
    # Inspect shared ancestors, but never change their ACL to satisfy the SDK.
    $ancestors = foreach ($directory in @($volumeRoot, $env:ProgramData, $env:ProgramFiles)) {
        $descriptor = Get-Acl -LiteralPath $directory
        @{path=$directory; owner=$descriptor.Owner; sddl=$descriptor.Sddl}
    }
    $ancestors | ConvertTo-Json -Depth 5 | Set-Content (Join-Path $out 'windows-ancestor-acls.json')
    # Only the disposable account receives access; scanning stays non-admin.
    & icacls.exe $root /grant "*${sid}:(OI)(CI)(RX)" /T /C | Out-File (Join-Path $out 'windows-acl.log')
    if ($LASTEXITCODE -ne 0) { throw 'Could not grant read access to the test payload' }
    & icacls.exe $runtime.root /grant "*${sid}:(OI)(CI)(RX)" /T /C | Out-File (Join-Path $out 'windows-runtime-acl.log')
    if ($LASTEXITCODE -ne 0) { throw 'Could not grant read access to the test runtime' }
    # Reports contain synthetic evidence, not credential state. Keep the
    # uploader's existing access and add only the fixture user's write access.
    & icacls.exe $out /grant "*${sid}:(OI)(CI)(M)" /T /C | Out-File (Join-Path $out 'windows-private-acl.log')
    if ($LASTEXITCODE -ne 0) { throw 'Could not grant fixture evidence access' }
    # Private SDK state is separate from reports. Construct its DACL explicitly
    # and do not let the launcher's ordinary user SID inherit into children.
    $acl = [Security.AccessControl.DirectorySecurity]::new()
    $acl.SetAccessRuleProtection($true, $false)
    $acl.SetOwner([Security.Principal.SecurityIdentifier]::new('S-1-5-32-544'))
    foreach ($identity in @($sid, 'S-1-5-18', 'S-1-5-32-544')) {
        $rule = [Security.AccessControl.FileSystemAccessRule]::new([Security.Principal.SecurityIdentifier]::new($identity), [Security.AccessControl.FileSystemRights]::FullControl, [Security.AccessControl.InheritanceFlags]'ContainerInherit,ObjectInherit', [Security.AccessControl.PropagationFlags]::None, [Security.AccessControl.AccessControlType]::Allow)
        $acl.AddAccessRule($rule)
    }
    $acl.AddAccessRule([Security.AccessControl.FileSystemAccessRule]::new([Security.Principal.SecurityIdentifier]::new($runnerSid), [Security.AccessControl.FileSystemRights]::ReadAndExecute, [Security.AccessControl.AccessControlType]::Allow))
    Set-Acl -LiteralPath $workParent -AclObject $acl
    $credential = [PSCredential]::new("$env:COMPUTERNAME\$user", $password)
    $homeArguments = ('"' + (Join-Path $root 'deploy/native/windows-home.mjs') + '" "' + $workParent + '"')
    $homeProcess = Start-Process -FilePath $runtime.node -ArgumentList $homeArguments -Credential $credential -LoadUserProfile -WorkingDirectory $workParent -PassThru
    if (-not $homeProcess.WaitForExit(30000)) { throw 'Private fixture home initialization did not finish' }
    if ($homeProcess.ExitCode -ne 0) { throw 'Private fixture home initialization failed' }
    # Administrative setup uses a hash-verified CLI copied to Program Files.
    # The subsequent SDK run remains under the ordinary fixture account.
    $platformPackage = (Get-Content (Join-Path $root 'deploy/sdk/manifest.json') -Raw | ConvertFrom-Json).packages | Where-Object { $_.platform -eq 'win32-x64' }
    $module = Join-Path $runtime.root 'sdk\@openai\codex-win32-x64'
    New-Item -ItemType Directory -Path $module -Force | Out-Null
    Copy-Item (Join-Path $root 'code-audit-scanner\node_modules\@openai\codex-win32-x64\*') -Destination $module -Recurse -Force
    $relative = ($platformPackage.files.PSObject.Properties | Where-Object { $_.Name.EndsWith('/bin/codex.exe') }).Name
    $codex = Join-Path $module $relative
    if ((Get-FileHash -LiteralPath $codex -Algorithm SHA256).Hash.ToLowerInvariant() -ne $platformPackage.files.$relative) { throw 'Trusted CLI copy differs from the platform inventory' }
    $runtime | Add-Member -NotePropertyName codexModuleRoot -NotePropertyValue $module -Force
    $runtime | Add-Member -NotePropertyName codexPath -NotePropertyValue $codex -Force
    $runtime | Add-Member -NotePropertyName userHome -NotePropertyValue (Join-Path $workParent 'profile') -Force
    $runtime | Add-Member -NotePropertyName sdkStateRoot -NotePropertyValue (Join-Path $workParent 'sdk-state') -Force
    $runtime | ConvertTo-Json -Depth 10 | Set-Content (Join-Path $out 'runtime.json')
    & $codex sandbox setup --elevated --user "$env:COMPUTERNAME\$user" --codex-home (Join-Path $workParent 'sdk-state\codex-home') *> (Join-Path $out 'windows-sandbox-setup.log')
    @{exitCode=$LASTEXITCODE; backend='elevated'; user=$user; scanRunsAsAdministrator=$false} | ConvertTo-Json | Set-Content (Join-Path $out 'windows-sandbox-setup.json')
    $arguments = @(
        ('"' + (Join-Path $root 'deploy/native/probe.mjs') + '"'),
        '--out', ('"' + $out + '"'),
        '--runtime', ('"' + (Join-Path $out 'runtime.json') + '"'),
        '--work', ('"' + (Join-Path $workParent 'work') + '"')
    ) -join ' '
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
