package goflow

import (
	"sync"
	"sync/atomic"
)

type graph struct {
	name     string
	nodes    []*node
	entries  []*node
	canceled atomic.Bool
	wg       sync.WaitGroup
}

func newGraph(name string) *graph {
	return &graph{
		name:    name,
		nodes:   make([]*node, 0, 8),
		entries: make([]*node, 0, 4),
	}
}

func (g *graph) push(n *node) {
	g.nodes = append(g.nodes, n)
	n.g = g
}

func (g *graph) setup() {
	g.entries = g.entries[:0]
	for _, n := range g.nodes {
		n.joinCounter.Store(0)
	}
	for _, n := range g.nodes {
		n.setup()
		if len(n.dependents) == 0 {
			g.entries = append(g.entries, n)
		}
	}
}

func (g *graph) walk(fn func(*node)) {
	for _, n := range g.nodes {
		fn(n)
		if sf, ok := n.ptr.(*subflowData); ok && sf.lastGraph != nil {
			sf.lastGraph.walk(fn)
		}
	}
}
