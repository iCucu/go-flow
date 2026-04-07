package goflow

// TaskPriority controls scheduling order. Lower numeric value = higher priority.
type TaskPriority uint

const (
	HIGH   TaskPriority = iota + 1
	NORMAL
	LOW
)

// Task is the user-facing handle to a node in the DAG.
type Task struct {
	node *node
}

// Precede declares that tasks depend on this task.
// For condition nodes, the order maps to predicate return values: 0 -> tasks[0], 1 -> tasks[1], etc.
func (t *Task) Precede(tasks ...*Task) {
	if cond, ok := t.node.ptr.(*conditionData); ok {
		for _, task := range tasks {
			idx := uint(len(cond.mapper))
			cond.mapper[idx] = task.node
		}
	}
	for _, task := range tasks {
		t.node.precede(task.node)
	}
}

// Succeed declares that this task depends on the given tasks.
func (t *Task) Succeed(tasks ...*Task) {
	for _, task := range tasks {
		task.node.precede(t.node)
	}
}

// Priority sets the scheduling priority for this task.
func (t *Task) Priority(p TaskPriority) *Task {
	t.node.priority = p
	return t
}

// Name returns the task name.
func (t *Task) Name() string {
	return t.node.name
}
