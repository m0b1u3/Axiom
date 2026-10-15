package contracts

// SupportsResult requires the complete publication and completion chain, rather
// than assuming that support for job input implies support for result output.
func (c ResultCapabilities) SupportsResult(version int) bool {
	contains := func(values []int64, wanted int64) bool {
		for _, v := range values {
			if v == wanted {
				return true
			}
		}
		return false
	}
	return version == 1 && contains(c.JobVersions, 2) && contains(c.ResultVersions, 1) && contains(c.CompletionVersions, 1) && contains(c.CommitVersions, 1)
}

func NoResultCapabilities() ResultCapabilities {
	return ResultCapabilities{JobVersions: []int64{}, ResultVersions: []int64{}, CompletionVersions: []int64{}, CommitVersions: []int64{}}
}
