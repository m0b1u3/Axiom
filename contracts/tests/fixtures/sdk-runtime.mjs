// Contract-only CLI fixture. The SDK's actual package integrity and runtime
// selection are tested by Scanner; the contract job installs no SDK packages.
export async function bindSDKRuntime(task) {
  if (task.mock !== true) throw new Error('Contract runtime fixture requires --mock');
  task.sdkRuntime = {id:'official-v2',fingerprint:'f'.repeat(64)};
  return task.sdkRuntime;
}
