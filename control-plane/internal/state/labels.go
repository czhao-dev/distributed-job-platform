package state

// matchesSelector reports whether labels satisfies all key-value pairs in
// selector. An empty selector matches everything.
func matchesSelector(labels, selector map[string]string) bool {
	for k, v := range selector {
		if labels[k] != v {
			return false
		}
	}
	return true
}
