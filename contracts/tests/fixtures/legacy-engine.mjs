// Test-only scanner engine. Echoes legacy input and never contacts a provider.
export const SCAN_EXIT_CODES={completed:0};
export class CodexSecurityEngine {
  async scan(task) {return {status:'completed',findings:[],fixtureOnly:true,legacyTask:task};}
}
