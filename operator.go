package goflow

// Operator is the core abstraction for a computational unit in the DAG.
// Implement this interface to define reusable, composable, and testable task logic.
type Operator interface {
	Compute()
}

// FuncOperator adapts a plain function into an Operator.
type FuncOperator func()

func (f FuncOperator) Compute() { f() }
