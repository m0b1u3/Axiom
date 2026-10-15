# Scan observability delivery

Deep scans pass the discovery time limit to the SDK through maxTimeHours and reserve ten additional minutes for finalization. Optional discovery-run and no-new-finding limits are supported. SDK worker defaults are retained.

Deep counters, worker observations, and the latest reconnect event are preserved in progress and execution history. Coverage is archived with each execution and available through the authenticated coverage endpoint; older executions can read retained coverage artifacts.

Validation: 71 scanner tests, 8 frontend tests, full Go tests against isolated PostgreSQL, production builds, and desktop/mobile checks passed. A standard scan completed with one finding. A deep scan confirmed the native policy and entered SDK reduction; its complete finalization was not verified because the user requested stopping paid testing. All paid test processes were stopped. No further model-based acceptance runs are authorized by this delivery.
