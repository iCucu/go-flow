package goflow

import (
	"fmt"
	"io"
	"strings"
)

// dumpDOT writes the graph as a Graphviz DOT document.
// Subflows that have been executed at least once are rendered as DOT subgraph clusters.
func dumpDOT(g *graph, w io.Writer) error {
	var sb strings.Builder
	sb.WriteString("digraph ")
	sb.WriteString(quote(g.name))
	sb.WriteString(" {\n")

	dumpGraphDOT(g, &sb, "  ")

	sb.WriteString("}\n")
	_, err := io.WriteString(w, sb.String())
	return err
}

// dumpGraphDOT recursively writes nodes, edges, and subflow clusters.
func dumpGraphDOT(g *graph, sb *strings.Builder, indent string) {
	// Node declarations
	for _, n := range g.nodes {
		attrs := nodeAttrs(n)
		sb.WriteString(fmt.Sprintf("%s%s%s;\n", indent, quote(n.name), attrs))
	}

	// Edges (condition nodes annotate edges with branch index)
	for _, n := range g.nodes {
		for i, succ := range n.successors {
			label := ""
			if n.typ == nodeTypeCondition {
				label = fmt.Sprintf(" [label=%d]", i)
			}
			sb.WriteString(fmt.Sprintf("%s%s -> %s%s;\n", indent, quote(n.name), quote(succ.name), label))
		}
	}

	// Subflow clusters (only if the subflow has been executed at least once)
	for _, n := range g.nodes {
		if sf, ok := n.ptr.(*subflowData); ok && sf.lastGraph != nil {
			sb.WriteString(fmt.Sprintf("%ssubgraph %s {\n", indent, quote("cluster_"+n.name)))
			sb.WriteString(fmt.Sprintf("%s  label=%s;\n", indent, quote(n.name)))
			dumpGraphDOT(sf.lastGraph, sb, indent+"  ")
			sb.WriteString(fmt.Sprintf("%s}\n", indent))
		}
	}
}

// nodeAttrs returns DOT attributes for the node's visual shape.
func nodeAttrs(n *node) string {
	switch n.typ {
	case nodeTypeCondition:
		return " [shape=diamond]"
	case nodeTypeSubflow:
		return " [shape=doubleoctagon]"
	default:
		return ""
	}
}

func quote(s string) string {
	return `"` + s + `"`
}
