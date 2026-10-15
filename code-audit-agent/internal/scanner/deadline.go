package scanner

// Keep the native discovery deadline separate from the bounded finalization deadline.
func HardTimeoutMinutes(mode string, discovery int) int {
	if mode == "deep" {
		return discovery + 10
	}
	return discovery
}
