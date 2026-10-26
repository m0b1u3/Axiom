// Native Codex provider auth command: stdout is consumed privately by Codex.
// Arguments contain an environment variable name, never the credential itself.
const name=process.argv[2];
const token=name&&/^[A-Z][A-Z0-9_]{0,127}$/.test(name)?process.env[name]?.trim():undefined;
if(!token||/[\u0000-\u0020\u007f]/u.test(token)){
 process.stderr.write('Selected model credential is missing or invalid.\n');
 process.exitCode=1;
}else process.stdout.write(token+'\n');
