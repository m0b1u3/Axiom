$ErrorActionPreference = 'Stop'
$out = Join-Path $env:RUNNER_TEMP 'native-stage0-out'
New-Item -ItemType Directory -Path $out -Force | Out-Null
$downloads = Join-Path $env:RUNNER_TEMP 'native-stage0-prerequisites'
python deploy/native/fetch.py --lock deploy/native/system-prerequisites.lock.json --target win32-x64 --destination $downloads
if ($LASTEXITCODE -ne 0) { throw 'System prerequisite did not match the frozen input' }
$lock = Get-Content deploy/native/system-prerequisites.lock.json -Raw | ConvertFrom-Json
$asset = $lock.assets[0]
$path = Join-Path $downloads $asset.filename
$signature = Get-AuthenticodeSignature -LiteralPath $path
if ($signature.Status -ne 'Valid' -or $signature.SignerCertificate.Subject -notmatch 'Microsoft Corporation') {
    throw 'Microsoft runtime installer did not have a valid Microsoft Authenticode signature'
}
$fileVersion = (Get-Item -LiteralPath $path).VersionInfo.FileVersion
if ($fileVersion -ne $asset.version) { throw 'Microsoft runtime file version differs from the frozen input' }
$system = @{}
foreach ($name in @('vcruntime140.dll', 'vcruntime140_1.dll', 'msvcp140.dll')) {
    $dll = Join-Path $env:SystemRoot "System32\$name"
    $system[$name] = if (Test-Path -LiteralPath $dll) { (Get-Item -LiteralPath $dll).VersionInfo.FileVersion } else { $null }
}
@{status='verified'; installerVersion=$fileVersion; sha256=$asset.sha256; signatureStatus=[string]$signature.Status; signer=$signature.SignerCertificate.Subject; installedByStage0=$false; runnerSystemDllVersions=$system} | ConvertTo-Json -Depth 5 | Set-Content (Join-Path $out 'windows-prerequisites.json')
