package contracts

// NeedsSDKConcurrency marks values that older runtimes reject. The current
// schemas use the SDK's safe-integer domain; these are compatibility thresholds,
// never dispatch quotas or values to clamp an operator's configuration to.
func NeedsSDKConcurrency(policy ScanPolicy, input *ScanInput, components *ComponentScanRequest) bool {
	return (policy.Workers.Present && policy.Workers.Value > SDKLegacyWorkers) ||
		(policy.DeepMaxDiscoveryRuns.Present && policy.DeepMaxDiscoveryRuns.Value > SDKLegacyDeepCounts) ||
		(policy.DeepStopAfterNoNew.Present && policy.DeepStopAfterNoNew.Value > SDKLegacyDeepCounts) ||
		(input != nil && input.Subagents.Present && input.Subagents.Value > SDKLegacySubagents) ||
		(components != nil && components.Workers > SDKLegacyComponentWorkers)
}

func DescriptionSupportsSDKParameters(version int64) bool {
	return version == SDKParametersDescriptionVersion || DescriptionSupportsSDKConcurrency(version)
}

func DescriptionSupportsSDKIdentity(version int64) bool {
	return version == SDKIdentityDescriptionVersion || DescriptionSupportsSDKParameters(version)
}

// Description 13 adds the 0.5.0 result adapter without imposing new concurrency caps.
func DescriptionSupportsSDKConcurrency(version int64) bool {
	return version == SDKConcurrencyDescriptionVersion || version == SDKAdapterDescriptionVersion || version == 14 || version == 15
}
