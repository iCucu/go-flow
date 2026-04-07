package goflow

import (
	"sync"
	"sync/atomic"
)

// graph is the internal DAG container. It holds all nodes, identifies entry points
// (nodes with no predecessors), and tracks graph-level completion via sync.WaitGroup.
// A single graph may represent a top-level TaskFlow or a dynamically created subflow.
type graph struct {
	name     string
	nodes    []*node
	entries  []*node      // populated by setup(); nodes with len(dependents) == 0
	canceled atomic.Bool  // set to true when any task in this graph panics
	wg       sync.WaitGroup
}

func newGraph(name string) *graph {
	return &graph{
		name:    name,
		nodes:   make([]*node, 0, 8),
		entries: make([]*node, 0, 4),
	}
}

// push adds a node to this graph and binds the node's graph pointer.
func (g *graph) push(n *node) {
	g.nodes = append(g.nodes, n)
	n.g = g
}

// setup prepares the graph for execution: resets all joinCounters,
// re-computes dependency counts, and identifies entry nodes.
// Must be called before scheduling.
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

// walk visits every node in the graph, recursing into instantiated subflows.
// Used by the DOT visualizer.
func (g *graph) walk(fn func(*node)) {
	for _, n := range g.nodes {
		fn(n)
		if sf, ok := n.ptr.(*subflowData); ok && sf.lastGraph != nil {
			sf.lastGraph.walk(fn)
		}
	}
}
