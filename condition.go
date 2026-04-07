package goflow

type conditionData struct {
	handle func() uint
	mapper map[uint]*node
}
