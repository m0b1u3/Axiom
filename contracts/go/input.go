package contracts

// SupportsInput centralizes admission semantics for versioned ScannerJob input.
func (c InputCapabilities) SupportsInput(version SchemaVersion, budget bool) bool {
	assignment, job := false, false
	for _, v := range c.AssignmentVersions {
		assignment = assignment || v == version
	}
	for _, v := range c.JobVersions {
		job = job || v == version
	}
	return assignment && job && (!budget || c.MaxCostUSD)
}
