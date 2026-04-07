package goflow

// conditionData holds the predicate function and the branch mapping.
// The predicate returns a uint index; mapper maps each index to the successor node
// that should be scheduled when that index is chosen.
type conditionData struct {
	handle func() uint
	mapper map[uint]*node
}
