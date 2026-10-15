/** Agent supplies these values only for its separately owned Unix process group.
 * They are process supervision metadata, outside the scan input contract.
 */
export function superviseAgent(): () => void {
  const expected = process.env.AXIOM_SCANNER_PARENT_PID;
  if (!expected) return () => {};
  const parent = Number(expected);
  if (!Number.isSafeInteger(parent) || parent < 1 || process.env.AXIOM_SCANNER_OWN_PROCESS_GROUP !== "1" || process.platform === "win32") {
    throw new Error("Invalid Scanner process supervision configuration");
  }
  const check = (): void => {
    if (process.ppid === parent) return;
    process.stderr.write("Agent process exited; stopping Scanner and its SDK subprocesses\n");
    // Scanner is the group leader (Go Setpgid). Stopping the entire group also
    // stops native SDK workers; a live orphan must not keep making model calls.
    process.kill(-process.pid, "SIGKILL");
  };
  check();
  const timer = setInterval(check, 1000);
  timer.unref();
  return () => clearInterval(timer);
}
