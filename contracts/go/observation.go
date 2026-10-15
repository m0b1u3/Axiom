package contracts

// Observation support is negotiated separately from input and final results.
func (c ObservationCapabilities) SupportsObservation(version int) bool {
	contains := func(values []int64, wanted int64) bool {
		for _, v := range values {
			if v == wanted {
				return true
			}
		}
		return false
	}
	return version == 1 && contains(c.JobVersions, 3) && contains(c.ProgressVersions, 2) && contains(c.EventVersions, 2) && contains(c.HeartbeatVersions, 1)
}
func NoObservationCapabilities() ObservationCapabilities {
	return ObservationCapabilities{JobVersions: []int64{}, ProgressVersions: []int64{}, EventVersions: []int64{}, HeartbeatVersions: []int64{}}
}
