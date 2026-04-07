package goflow

import (
	"strconv"
	"sync/atomic"
	"time"
)

type nodeState int32

const (
	nodeIdle     nodeState = iota + 1
	nodeWaiting
	nodeRunning
	nodeFinished
)

type nodeType int

const (
	nodeTypeStatic nodeType = iota
	nodeTypeSubflow
	nodeTypeCondition
)

type node struct {
	name        string
	typ         nodeType
	successors  []*node
	dependents  []*node
	ptr         any
	state       atomic.Int32
	joinCounter atomic.Int32
	g           *graph
	priority    TaskPriority
}

func newNode(name string) *node {
	if len(name) == 0 {
		name = "N_" + strconv.Itoa(time.Now().Nanosecond())
	}
	n := &node{
		name:       name,
		successors: make([]*node, 0, 4),
		dependents: make([]*node, 0, 4),
		priority:   NORMAL,
	}
	n.state.Store(int32(nodeIdle))
	return n
}

func (n *node) recyclable() bool {
	return n.joinCounter.Load() == 0
}

func (n *node) setup() {
	n.state.Store(int32(nodeIdle))
	for _, dep := range n.dependents {
		if dep.typ == nodeTypeCondition {
			continue
		}
		n.joinCounter.Add(1)
	}
}

func (n *node) drop() {
	if n.typ == nodeTypeCondition {
		return
	}
	for _, succ := range n.successors {
		succ.joinCounter.Add(-1)
	}
}

// rearm re-establishes the joinCounter so the node can be re-executed in cyclic flows.
func (n *node) rearm() {
	var cnt int32
	for _, dep := range n.dependents {
		if dep.typ == nodeTypeCondition {
			continue
		}
		cnt++
	}
	n.joinCounter.Store(cnt)
}

func (n *node) precede(v *node) {
	n.successors = append(n.successors, v)
	v.dependents = append(v.dependents, n)
}

func (n *node) hasCondPredecessor() bool {
	for _, dep := range n.dependents {
		if dep.typ == nodeTypeCondition {
			return true
		}
	}
	return false
}
