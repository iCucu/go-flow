package goflow

import "io"

// TaskFlow represents a DAG of tasks.
type TaskFlow struct {
	graph  *graph
	frozen bool
}

// NewTaskFlow creates a new named TaskFlow.
func NewTaskFlow(name string) *TaskFlow {
	return &TaskFlow{graph: newGraph(name)}
}

// Name returns the taskflow name.
func (tf *TaskFlow) Name() string {
	return tf.graph.name
}

// Reset allows a frozen taskflow to accept new tasks again.
func (tf *TaskFlow) Reset() {
	tf.frozen = false
}

func (tf *TaskFlow) push(tasks ...*Task) {
	if tf.frozen {
		panic("taskflow is frozen, cannot add tasks")
	}
	for _, task := range tasks {
		tf.graph.push(task.node)
	}
}

// NewTask adds a static task to this taskflow.
func (tf *TaskFlow) NewTask(name string, f func()) *Task {
	task := &Task{node: newStaticNode(name, f)}
	tf.push(task)
	return task
}

// NewOperatorTask adds an Operator-based task to this taskflow.
func (tf *TaskFlow) NewOperatorTask(name string, op Operator) *Task {
	task := &Task{node: newOperatorNode(name, op)}
	tf.push(task)
	return task
}

// NewSubflow adds a dynamic subflow task. The callback is invoked on every execution.
func (tf *TaskFlow) NewSubflow(name string, f func(sf *Subflow)) *Task {
	task := &Task{node: newSubflowNode(name, f)}
	tf.push(task)
	return task
}

// NewCondition adds a condition task whose return value selects the successor.
func (tf *TaskFlow) NewCondition(name string, predict func() uint) *Task {
	task := &Task{node: newConditionNode(name, predict)}
	tf.push(task)
	return task
}

// Dump writes a DOT representation of the graph to w.
func (tf *TaskFlow) Dump(w io.Writer) error {
	return dumpDOT(tf.graph, w)
}
