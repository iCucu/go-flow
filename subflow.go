package goflow

// subflowData holds the user's instantiation callback and the last-built graph.
// Unlike static subflows, the callback is re-invoked on every execution,
// producing a fresh graph each time. lastGraph is retained for DOT visualization.
type subflowData struct {
	handle    func(sf *Subflow)
	lastGraph *graph
}

// Subflow allows users to dynamically build a sub-DAG each time the subflow runs.
// It exposes the same task creation API as TaskFlow (NewTask, NewOperatorTask,
// NewSubflow, NewCondition) scoped to the subflow's internal graph.
type Subflow struct {
	g *graph
}

func (sf *Subflow) push(tasks ...*Task) {
	for _, task := range tasks {
		sf.g.push(task.node)
	}
}

// NewTask adds a static task (plain function) to this subflow.
func (sf *Subflow) NewTask(name string, f func()) *Task {
	task := &Task{node: newStaticNode(name, f)}
	sf.push(task)
	return task
}

// NewSubflow adds a nested subflow. Subflows can be arbitrarily nested.
func (sf *Subflow) NewSubflow(name string, f func(sf *Subflow)) *Task {
	task := &Task{node: newSubflowNode(name, f)}
	sf.push(task)
	return task
}

// NewCondition adds a condition node to this subflow.
func (sf *Subflow) NewCondition(name string, predict func() uint) *Task {
	task := &Task{node: newConditionNode(name, predict)}
	sf.push(task)
	return task
}

// NewOperatorTask adds an Operator-based task to this subflow.
func (sf *Subflow) NewOperatorTask(name string, op Operator) *Task {
	task := &Task{node: newOperatorNode(name, op)}
	sf.push(task)
	return task
}

// newStaticNode wraps a plain func() as a FuncOperator and creates an operator node.
func newStaticNode(name string, f func()) *node {
	return newOperatorNode(name, FuncOperator(f))
}

// newOperatorNode creates a node backed by an Operator implementation.
func newOperatorNode(name string, op Operator) *node {
	n := newNode(name)
	n.typ = nodeTypeStatic
	n.ptr = &staticData{op: op}
	return n
}

// newSubflowNode creates a subflow node with a dynamic instantiation callback.
func newSubflowNode(name string, f func(sf *Subflow)) *node {
	n := newNode(name)
	n.typ = nodeTypeSubflow
	n.ptr = &subflowData{handle: f}
	return n
}

// newConditionNode creates a condition node with a predicate and an empty branch mapper.
func newConditionNode(name string, f func() uint) *node {
	n := newNode(name)
	n.typ = nodeTypeCondition
	n.ptr = &conditionData{handle: f, mapper: make(map[uint]*node)}
	return n
}

// staticData wraps an Operator. Both NewTask (via FuncOperator) and NewOperatorTask
// produce staticData; the executor calls op.Compute() uniformly.
type staticData struct {
	op Operator
}
