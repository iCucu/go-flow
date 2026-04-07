package goflow

// subflowData holds the user's instantiation callback and the last-built graph
// for visualization purposes. The graph is rebuilt on every execution.
type subflowData struct {
	handle    func(sf *Subflow)
	lastGraph *graph
}

// Subflow allows users to dynamically build a sub-DAG each time the subflow runs.
type Subflow struct {
	g *graph
}

func (sf *Subflow) push(tasks ...*Task) {
	for _, task := range tasks {
		sf.g.push(task.node)
	}
}

// NewTask adds a static task to this subflow.
func (sf *Subflow) NewTask(name string, f func()) *Task {
	task := &Task{node: newStaticNode(name, f)}
	sf.push(task)
	return task
}

// NewSubflow adds a nested subflow to this subflow.
func (sf *Subflow) NewSubflow(name string, f func(sf *Subflow)) *Task {
	task := &Task{node: newSubflowNode(name, f)}
	sf.push(task)
	return task
}

// NewCondition adds a condition task to this subflow.
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

func newStaticNode(name string, f func()) *node {
	return newOperatorNode(name, FuncOperator(f))
}

func newOperatorNode(name string, op Operator) *node {
	n := newNode(name)
	n.typ = nodeTypeStatic
	n.ptr = &staticData{op: op}
	return n
}

func newSubflowNode(name string, f func(sf *Subflow)) *node {
	n := newNode(name)
	n.typ = nodeTypeSubflow
	n.ptr = &subflowData{handle: f}
	return n
}

func newConditionNode(name string, f func() uint) *node {
	n := newNode(name)
	n.typ = nodeTypeCondition
	n.ptr = &conditionData{handle: f, mapper: make(map[uint]*node)}
	return n
}

// staticData wraps an Operator.
type staticData struct {
	op Operator
}
