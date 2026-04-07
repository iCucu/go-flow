# go-flow

[中文文档](README_zh.md)

A high-performance DAG task-parallel execution framework for Go, inspired by [go-taskflow](https://github.com/noneback/go-taskflow).

## Features

- **Operator abstraction** -- Implement the `Operator` interface to create stateful, reusable, testable compute units as DAG nodes
- **Dynamic Subflows** -- Subflow callbacks are re-invoked on every execution, producing different DAG topologies at runtime
- **Static tasks, conditional branching, loops** -- Covers typical DAG orchestration patterns
- **Priority scheduling** -- Higher-priority tasks are dequeued and executed first
- **Panic isolation** -- A panicking task cancels its own subgraph without affecting others
- **DOT visualization** -- Dump graphs in Graphviz DOT format for SVG rendering
- **Zero external dependencies** -- Built entirely on the Go standard library

## Installation

```bash
go get github.com/iCucu/go-flow
```

Requires Go 1.22+.

---

## Quick Start

### Basic DAG

```go
package main

import (
    "fmt"
    "runtime"

    goflow "github.com/iCucu/go-flow"
)

func main() {
    tf := goflow.NewTaskFlow("pipeline")

    A := tf.NewTask("A", func() { fmt.Println("A") })
    B := tf.NewTask("B", func() { fmt.Println("B") })
    C := tf.NewTask("C", func() { fmt.Println("C") })
    D := tf.NewTask("D", func() { fmt.Println("D") })

    //   A
    //  / \
    // B   C
    //  \ /
    //   D
    A.Precede(B, C)
    D.Succeed(B, C)

    executor := goflow.NewExecutor(uint(runtime.NumCPU()))
    executor.Run(tf).Wait()
}
```

### Operator

Implement the `Operator` interface to use structured, stateful compute units as task nodes:

```go
type Operator interface {
    Compute()
}
```

Example:

```go
type Adder struct {
    Val    int
    Target *atomic.Int64
}

func (a *Adder) Compute() {
    a.Target.Add(int64(a.Val))
}

var total atomic.Int64
tf := goflow.NewTaskFlow("operators")
a := tf.NewOperatorTask("add10", &Adder{Val: 10, Target: &total})
b := tf.NewOperatorTask("add20", &Adder{Val: 20, Target: &total})
a.Precede(b)
goflow.NewExecutor(4).Run(tf).Wait()
// total == 30
```

Operators and plain `func()` tasks are fully interchangeable and share the same dependency API. The built-in `FuncOperator` adapter wraps a plain function as an Operator:

```go
a := tf.NewTask("plain", func() { /* ... */ })
b := tf.NewOperatorTask("op", myOperator)
c := tf.NewOperatorTask("wrapped", goflow.FuncOperator(func() { /* ... */ }))
a.Precede(b)
b.Precede(c)
```

### Dynamic Subflows

The subflow callback is invoked on **every execution**, allowing DAG topology to change based on runtime state:

```go
var iteration int

tf := goflow.NewTaskFlow("dynamic")
tf.NewSubflow("batch", func(sf *goflow.Subflow) {
    iteration++
    for i := 0; i < iteration*2; i++ {
        sf.NewTask(fmt.Sprintf("worker-%d", i), func() { /* ... */ })
    }
})

executor := goflow.NewExecutor(8)
for i := 0; i < 3; i++ {
    tf.Reset()
    executor.Run(tf).Wait()
    // Run 1: 2 workers, Run 2: 4, Run 3: 6
}
```

Subflows support `NewOperatorTask`, nested `NewSubflow`, and `NewCondition`.

### Conditional Branching and Loops

```go
tf := goflow.NewTaskFlow("loop")

var counter int
init := tf.NewTask("init", func() { counter = 0 })
body := tf.NewTask("body", func() { counter++ })
done := tf.NewTask("done", func() { fmt.Println("finished:", counter) })

cond := tf.NewCondition("check", func() uint {
    if counter < 10 {
        return 0 // continue
    }
    return 1     // exit
})

init.Precede(body)
body.Precede(cond)
cond.Precede(body, done) // 0 -> body (loop), 1 -> done (exit)

goflow.NewExecutor(4).Run(tf).Wait()
```

### Priority Scheduling

```go
tf := goflow.NewTaskFlow("prio")
start := tf.NewTask("start", func() {})

high := tf.NewTask("high", func() {}).Priority(goflow.HIGH)
low  := tf.NewTask("low",  func() {}).Priority(goflow.LOW)

start.Precede(high, low)
```

### DOT Visualization

```go
tf.Dump(os.Stdout)
// Outputs Graphviz DOT format; render with: dot -Tsvg
```

---

## Architecture

### Layers

```
┌──────────────────────────────────────────────────┐
│              User API Layer                      │
│   TaskFlow · Task · Subflow · Operator           │
├──────────────────────────────────────────────────┤
│              Core Layer                          │
│   graph · node · staticData(Operator)            │
│   subflowData · conditionData                    │
├──────────────────────────────────────────────────┤
│           Execution Layer                        │
│   Executor · pool (FIFO worker pool)             │
└──────────────────────────────────────────────────┘
```

### Core Data Structures

**graph** -- Internal DAG container holding all nodes and entry nodes, using `sync.WaitGroup` for completion tracking.

**node** -- Execution unit with an `atomic.Int32` state machine and dependency counter. No per-node mutex:

```go
type node struct {
    name        string
    typ         nodeType       // static | subflow | condition
    successors  []*node
    dependents  []*node
    ptr         any            // *staticData(Operator) | *subflowData | *conditionData
    state       atomic.Int32   // idle → waiting → running → finished → idle
    joinCounter atomic.Int32   // pending predecessor count
    g           *graph
    priority    TaskPriority
}
```

**staticData** -- Wraps the `Operator` interface. `NewTask(name, func())` adapts via `FuncOperator` internally; `NewOperatorTask(name, op)` holds the user implementation directly. Both are identical at the scheduling layer.

State transitions:

```
            ┌────────────────────────────────┐
            │            rearm()             │
            ▼                                │
    ┌──────────┐  CAS   ┌──────────┐  ┌──────────┐  ┌──────────┐
    │   Idle   │ ────►  │ Waiting  │  │ Running  │  │ Finished │
    └──────────┘        └────┬─────┘  └────┬─────┘  └────┬─────┘
                             │ schedule()  │ invoke()     │
                             └─────────────┘              │
                                                          │ staticFinished() / conditionFinished()
                                                          └──────────────────────────┘
```

### Task Types

| Type | Creation | Scheduling Behavior |
|---|---|---|
| **Static / Operator** | `NewTask` / `NewOperatorTask` | Scheduled when all predecessors complete; calls `Operator.Compute()`; triggers successors |
| **Subflow** | `NewSubflow` | Creates a fresh graph each run; invokes callback to build topology |
| **Condition** | `NewCondition` | `func() uint` return value selects a single successor; supports loops |

### Execution Flow

```
Executor.Run(tf)
  │
  ├── tf.frozen = true             // freeze: no new tasks allowed
  ├── graph.setup()                // reset joinCounters, identify entry nodes
  ├── schedule(entries...)         // submit entry nodes to pool
  │
  │   ┌─── pool.Go(invokeNode) ───────────────────┐
  │   │                                            │
  │   │  invokeStatic:                             │
  │   │    op.Compute()                            │
  │   │    drop() → decrement successors' counters │
  │   │    scheduleSuccessors() → CAS ready nodes  │
  │   │    rearm() → reset joinCounter for cycles  │
  │   │                                            │
  │   │  invokeSubflow:                            │
  │   │    create fresh graph + Subflow            │
  │   │    call user callback to populate subgraph │
  │   │    scheduleGraph(subgraph) → recurse       │
  │   │                                            │
  │   │  invokeCondition:                          │
  │   │    run predicate → choice                  │
  │   │    rearm() → reset self                    │
  │   │    schedule(mapper[choice])                │
  │   └────────────────────────────────────────────┘
  │
  └── graph.wg.Wait()              // block until all nodes complete
```

---

## Performance Optimizations

### 1. Lock-Free Node State Transitions (CAS)

All node state transitions use `atomic.CompareAndSwap` with no mutexes:

```go
if succ.recyclable() && succ.state.CompareAndSwap(int32(nodeIdle), int32(nodeWaiting)) {
    candidates = append(candidates, succ)
}
```

CAS merges "check state + update state" into a single atomic instruction. When multiple predecessors finish concurrently and try to schedule the same successor, only one CAS succeeds, naturally preventing double-scheduling.

### 2. WaitGroup-Based Graph Completion

Graph-level completion uses `sync.WaitGroup` instead of a `sync.Cond` loop:

```go
e.wg.Add(1)    // on schedule
n.g.wg.Add(1)

n.g.wg.Done()  // on finish
e.wg.Done()

g.wg.Wait()    // wait for completion
```

WaitGroup is backed by atomics + a semaphore, lighter than the Cond loop's Lock → Wait → Signal → Unlock cycle. The advantage is most pronounced in sequential chains where each node immediately triggers the next.

### 3. Non-Blocking FIFO Worker Pool

```go
func (p *pool) Go(f func()) {
    p.mu.Lock()
    p.queue = append(p.queue, f)
    if p.workers < p.cap {
        p.workers++
        p.mu.Unlock()
        go p.run()
    } else {
        p.mu.Unlock()
    }
}
```

- **Submissions never block the caller**: prevents deadlocks when tasks submit sub-tasks into the same pool
- **Workers created on demand, exit when idle**: avoids wasting resources on idle goroutines
- **FIFO ordering**: priority-sorted tasks dequeue in the correct order

### 4. Atomic Rearm for Cyclic Flows

Condition-driven loops require resetting dependency counters after each iteration, done with pure atomics:

```go
func (n *node) rearm() {
    var cnt int32
    for _, dep := range n.dependents {
        if dep.typ == nodeTypeCondition { continue }
        cnt++
    }
    n.joinCounter.Store(cnt)
}
```

For condition nodes, `rearm()` runs **before** scheduling the successor, ensuring the successor's `drop()` sees the correct joinCounter value, eliminating race conditions by design.

### 5. Low Memory Allocation

| Optimization | Details |
|---|---|
| Pre-allocated slices | `successors` / `dependents` start with capacity 4 |
| No per-node Mutex | Node state managed entirely via atomics |
| No sync.Cond | Graph completion uses WaitGroup |
| Direct closure dispatch | No intermediate Object Pool wrapper |
| Zero-cost Operator | `NewTask` uses a `FuncOperator` type conversion, no extra allocation |

---

## Dynamic Subflow Design

go-flow creates a fresh graph and re-invokes the user callback on every subflow execution:

```go
sg := newGraph(n.name)
sf := &Subflow{g: sg}
e.safeCall(n, func() { p.handle(sf) })
p.lastGraph = sg
e.scheduleGraph(n.g, sg)
```

The callback can dynamically decide subgraph topology, task count, and dependencies based on runtime state. Each execution may produce a completely different DAG.

**Typical use cases:**

- **Data sharding** -- Dynamically create N parallel workers based on input size
- **Conditional pipelines** -- Build different processing pipelines based on upstream results
- **Recursive divide-and-conquer** -- Nest subflows within subflows for recursive parallelism

---

## Error Handling

Unrecovered `panic` in tasks is caught by the framework:

1. The current graph is marked `canceled`
2. Unscheduled tasks are skipped
3. Already-running tasks are unaffected
4. Subgraph cancellation propagates to the parent graph

```go
tf.NewTask("safe", func() {
    defer func() {
        if r := recover(); r != nil {
            // User handles panic; framework will not cancel the graph
        }
    }()
    riskyOperation()
})
```

---

## API Reference

### TaskFlow

| Method | Signature | Description |
|---|---|---|
| `NewTaskFlow` | `(name string) *TaskFlow` | Create a named TaskFlow |
| `NewTask` | `(name string, f func()) *Task` | Add a function task |
| `NewOperatorTask` | `(name string, op Operator) *Task` | Add an Operator task |
| `NewSubflow` | `(name string, f func(sf *Subflow)) *Task` | Add a dynamic subflow |
| `NewCondition` | `(name string, f func() uint) *Task` | Add a condition node |
| `Dump` | `(w io.Writer) error` | Output DOT graph |
| `Reset` | `()` | Unfreeze for re-execution |
| `Name` | `() string` | Return the name |

### Task

| Method | Signature | Description |
|---|---|---|
| `Precede` | `(tasks ...*Task)` | Declare successors (condition nodes map return values in order) |
| `Succeed` | `(tasks ...*Task)` | Declare predecessors |
| `Priority` | `(p TaskPriority) *Task` | Set scheduling priority |
| `Name` | `() string` | Return the name |

### Executor

| Method | Signature | Description |
|---|---|---|
| `NewExecutor` | `(concurrency uint) Executor` | Create an executor (concurrency > 0) |
| `Run` | `(tf *TaskFlow) Executor` | Start execution |
| `Wait` | `()` | Block until all tasks complete |

### Operator

| Type | Description |
|---|---|
| `Operator` | Interface with a `Compute()` method |
| `FuncOperator` | `func()` type that implements `Operator`; adapts plain functions |

### Priority Constants

| Constant | Value | Description |
|---|---|---|
| `HIGH` | 1 | Highest priority, scheduled first |
| `NORMAL` | 2 | Default priority |
| `LOW` | 3 | Lowest priority |

---

## Benchmarks

Environment: Apple M4 Pro (14 cores), Go 1.24, macOS

Compared against: [go-taskflow](https://github.com/noneback/go-taskflow) v1.2.0

### Basic Scenarios (empty tasks, pure framework overhead)

| Scenario | go-flow | go-taskflow | Speedup | Allocs |
|---|---|---|---|---|
| C32 (32 concurrent) | 15,163 ns | 27,676 ns | **1.8x** | 84 vs 227 |
| S32 (32 sequential) | 9,906 ns | 49,024 ns | **4.9x** | 127 vs 286 |
| C6 (diamond DAG) | 2,006 ns | 7,634 ns | **3.8x** | 23 vs 52 |
| C8x8 (8 layers x 8) | 33,084 ns | 66,276 ns | **2.0x** | 234 vs 560 |

### Large Scale

| Scenario | go-flow | go-taskflow | Speedup | Allocs |
|---|---|---|---|---|
| C256 (256 concurrent) | 149,541 ns | 276,276 ns | **1.8x** | 654 vs 1,811 |
| C1024 (1024 concurrent) | 615,354 ns | 1,107,237 ns | **1.8x** | 2,615 vs 7,227 |
| S128 (128 sequential) | 40,996 ns | 189,521 ns | **4.6x** | 511 vs 1,150 |

### Topology Patterns

| Scenario | go-flow | go-taskflow | Speedup | Allocs |
|---|---|---|---|---|
| Fan-out/in 1→64→1 | 35,877 ns | 70,237 ns | **2.0x** | 232 vs 599 |
| Fan-out/in 1→256→1 | 157,583 ns | 300,079 ns | **1.9x** | 912 vs 2,340 |
| Pipeline 4x16 | 48,070 ns | 73,176 ns | **1.5x** | 212 vs 545 |
| Pipeline 8x32 | 338,346 ns | 483,679 ns | **1.4x** | 814 vs 2,274 |
| Pipeline 16x8 | 68,289 ns | 130,638 ns | **1.9x** | 478 vs 1,137 |
| Binary tree depth=6 | 140,944 ns | 168,253 ns | **1.2x** | 340 vs 1,087 |
| Diamond chain x16 | 17,203 ns | 62,843 ns | **3.7x** | 195 vs 439 |

### CPU-Bound Workload

| Scenario | go-flow | go-taskflow | Speedup |
|---|---|---|---|
| CPU C32 | 20,532 ns | 31,168 ns | **1.5x** |
| CPU Pipeline 4x8 | 19,296 ns | 37,671 ns | **2.0x** |

### Subflows and Loops

| Scenario | go-flow | go-taskflow | Speedup |
|---|---|---|---|
| Static subflow (4 nodes) | 2,975 ns | 6,610 ns | **2.2x** |
| Repeated run x10 | 70,903 ns | 124,082 ns | **1.7x** |

| Scenario | go-flow | Description |
|---|---|---|
| Dynamic subflow | 4,824 ns | Different task count per run |
| Nested subflow (3 levels) | 2,521 ns | Subflow within subflow |
| 8 concurrent subflows x 8 tasks | 80,856 ns | 8 parallel subflows, 8 tasks each |
| Condition loop x10 | 7,084 ns | 10 loop iterations |
| Condition loop x100 | 67,963 ns | 100 loop iterations |

### Summary

- **Sequential chains up to ~5x faster**: WaitGroup + CAS has significantly lower overhead on frequent wake-up paths
- **Concurrent scenarios consistently 1.5x -- 2.0x**: Lock-free scheduling reduces contention
- **56% -- 69% fewer allocations**: No per-node Mutex, no intermediate object wrappers
- **Dynamic subflows at zero extra cost**: Rebuilding the subgraph each run is as fast as static subflows

Full data and raw benchmark output: [REPORT.md](REPORT.md)

---

## File Structure

```
go-flow/
├── go.mod              # Module declaration
├── operator.go         # Operator interface + FuncOperator adapter
├── graph.go            # graph: DAG container, WaitGroup completion tracking
├── node.go             # node: atomic state machine, joinCounter, rearm
├── task.go             # Task: user API (Precede/Succeed/Priority)
├── taskflow.go         # TaskFlow: top-level container
├── subflow.go          # Subflow: dynamic subgraph, node factory functions
├── condition.go        # conditionData: branch mapping
├── executor.go         # Executor: scheduling core, CAS anti-double-schedule
├── pool.go             # pool: FIFO worker pool, on-demand workers
├── visualize.go        # DOT format output
├── goflow_test.go      # Unit tests
├── benchmark_test.go   # Benchmarks
└── REPORT.md           # Full test and performance report
```

## License

Apache-2.0
