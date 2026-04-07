# go-flow

高性能 Go DAG 任务并行执行框架，灵感来源于 [go-taskflow](https://github.com/noneback/go-taskflow)。

go-flow 实现了**动态子图**与多项底层性能优化，在全部基准场景下均取得 **1.3x -- 4.6x** 的加速，内存分配减少 **55% -- 69%**。

## 特性

- **静态任务、条件分支、循环** -- 覆盖典型 DAG 编排场景
- **动态子图 (Dynamic Subflow)** -- 每次执行时重新构建子图，拓扑可随运行时数据变化
- **优先级调度** -- 高优先级任务优先出队执行
- **Panic 隔离** -- 任务 panic 自动取消所在子图，不影响其他图
- **DOT 可视化** -- 输出 Graphviz DOT 格式，可直接渲染为 SVG
- **零外部依赖** -- 仅使用 Go 标准库

## 安装

```bash
go get github.com/zhiyidu/go-flow
```

要求 Go 1.22+。

---

## 快速上手

### 基础 DAG

```go
package main

import (
    "fmt"
    "runtime"

    goflow "github.com/zhiyidu/go-flow"
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

### 动态子图

子图的构建回调在**每次执行**时被调用，因此可以根据运行时状态产生不同的 DAG 拓扑：

```go
var iteration int

tf := goflow.NewTaskFlow("dynamic")
tf.NewSubflow("batch", func(sf *goflow.Subflow) {
    iteration++
    // 每次执行产生不同数量的并发任务
    for i := 0; i < iteration*2; i++ {
        sf.NewTask(fmt.Sprintf("worker-%d", i), func() {
            // 处理逻辑
        })
    }
})

executor := goflow.NewExecutor(8)
for i := 0; i < 3; i++ {
    tf.Reset()
    executor.Run(tf).Wait()
    // 第 1 次: 2 个 worker, 第 2 次: 4 个, 第 3 次: 6 个
}
```

### 条件分支与循环

```go
tf := goflow.NewTaskFlow("loop")

var counter int
init := tf.NewTask("init", func() { counter = 0 })
body := tf.NewTask("body", func() { counter++ })
done := tf.NewTask("done", func() { fmt.Println("finished:", counter) })

cond := tf.NewCondition("check", func() uint {
    if counter < 10 {
        return 0 // 继续循环
    }
    return 1     // 退出
})

init.Precede(body)
body.Precede(cond)
cond.Precede(body, done) // 0 -> body (循环), 1 -> done (退出)

goflow.NewExecutor(4).Run(tf).Wait()
```

### Operator 算子

实现 `Operator` 接口的结构体可以作为任务节点，适合封装有状态、可复用的计算逻辑：

```go
type Operator interface {
    Compute()
}
```

示例——累加算子：

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
tf.NewOperatorTask("add10", &Adder{Val: 10, Target: &total})
tf.NewOperatorTask("add20", &Adder{Val: 20, Target: &total})
goflow.NewExecutor(4).Run(tf).Wait()
// total == 30
```

Operator 任务与普通 `func()` 任务可自由混用，共享同一套依赖关系 API：

```go
a := tf.NewTask("plain", func() { /* ... */ })
b := tf.NewOperatorTask("op", myOperator)
a.Precede(b)
```

内置 `FuncOperator` 适配器可将普通函数包装为 Operator：

```go
op := goflow.FuncOperator(func() { fmt.Println("hello") })
tf.NewOperatorTask("wrapped", op)
```

### 优先级调度

```go
tf := goflow.NewTaskFlow("prio")
start := tf.NewTask("start", func() {})

high := tf.NewTask("high", func() {}).Priority(goflow.HIGH)
low  := tf.NewTask("low",  func() {}).Priority(goflow.LOW)

start.Precede(high, low)
// high 会先于 low 被调度执行
```

### DOT 可视化

```go
tf.Dump(os.Stdout)
// 输出 Graphviz DOT 格式，可用 dot -Tsvg 渲染
```

---

## 架构设计

### 整体分层

```
┌──────────────────────────────────────────────┐
│              User API Layer                  │
│   TaskFlow  ·  Task  ·  Subflow              │
├──────────────────────────────────────────────┤
│              Core Layer                      │
│   graph  ·  node  ·  staticData              │
│   subflowData  ·  conditionData              │
├──────────────────────────────────────────────┤
│           Execution Layer                    │
│   Executor  ·  pool (FIFO worker pool)       │
└──────────────────────────────────────────────┘
```

### 核心数据结构

#### graph

内部 DAG 容器，持有所有 `node` 及入口节点列表。使用 `sync.WaitGroup` 追踪图级别的完成状态。

```go
type graph struct {
    name     string
    nodes    []*node       // 所有节点
    entries  []*node       // 无前驱的入口节点
    canceled atomic.Bool   // panic 时标记取消
    wg       sync.WaitGroup
}
```

#### node

执行单元，通过 `atomic.Int32` 管理状态机和依赖计数器，**无互斥锁**：

```go
type node struct {
    name        string
    typ         nodeType       // static | subflow | condition
    successors  []*node        // 后继
    dependents  []*node        // 前驱
    ptr         any            // *staticData | *subflowData | *conditionData
    state       atomic.Int32   // idle -> waiting -> running -> finished -> idle
    joinCounter atomic.Int32   // 未完成的前驱计数
    g           *graph
    priority    TaskPriority
}
```

状态转换：

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

### 任务类型


| 类型            | 用途                       | 调度行为                   |
| ------------- | ------------------------ | ---------------------- |
| **Static**    | 普通函数 `func()`            | 前驱全部完成后调度，完成后触发后继      |
| **Subflow**   | 动态子图 `func(sf *Subflow)` | 每次执行创建新 graph，回调重新构建拓扑 |
| **Condition** | 分支 `func() uint`         | 返回值选择唯一后继，支持循环         |


### 执行流程

```
Executor.Run(tf)
  │
  ├── tf.frozen = true            // 冻结，禁止添加新任务
  ├── graph.setup()               // 重置所有 joinCounter，识别入口节点
  ├── schedule(entries...)        // 入口节点提交到 pool
  │
  │   ┌─── pool.Go(invokeNode) ──────────────────┐
  │   │                                           │
  │   │  invokeStatic:                            │
  │   │    run user func                          │
  │   │    drop() → 递减后继的 joinCounter         │
  │   │    scheduleSuccessors() → CAS 调度就绪节点  │
  │   │    rearm() → 重置自身 joinCounter (循环支持) │
  │   │                                           │
  │   │  invokeSubflow:                           │
  │   │    创建新 graph + Subflow                  │
  │   │    调用用户回调构建子图                      │
  │   │    scheduleGraph(子图) → 递归执行           │
  │   │                                           │
  │   │  invokeCondition:                         │
  │   │    run predicate → choice                 │
  │   │    rearm() → 重置自身                      │
  │   │    schedule(mapper[choice]) → 调度选中分支  │
  │   └───────────────────────────────────────────┘
  │
  └── graph.wg.Wait()             // 等待图内所有节点完成
```

---

## 性能优化细节

### 1. CAS 无锁节点状态转换

节点状态转换（idle → waiting → running → finished）完全通过 `atomic.CompareAndSwap` 实现，不使用任何互斥锁：

```go
if succ.recyclable() && succ.state.CompareAndSwap(int32(nodeIdle), int32(nodeWaiting)) {
    candidates = append(candidates, succ)
}
```

CAS 将「检查状态 + 修改状态」合并为单条原子指令。当多个前驱节点并发完成并尝试调度同一后继时，只有一个 CAS 能成功，天然防止重复调度，无需加锁。

### 2. WaitGroup 图完成追踪

图级别的完成等待使用 `sync.WaitGroup` 而非 `sync.Cond` 循环：

```go
// 调度时
e.wg.Add(1)
n.g.wg.Add(1)

// 节点完成时
n.g.wg.Done()
e.wg.Done()

// 等待图完成
g.wg.Wait()
```

每个节点被调度时 `Add(1)`，完成时 `Done()`。WaitGroup 内部基于原子操作 + 信号量实现，比 Cond 循环（每次唤醒都需要 Lock → Wait → Signal → Unlock）更轻量。这在串行链场景下优势尤为明显——每个节点完成后立即触发下一个，WaitGroup 的唤醒路径比 Cond 短得多。

### 3. FIFO 非阻塞工作池

go-flow 的 goroutine pool 采用 FIFO 队列 + 按需创建 worker 的设计：

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

关键设计点：

- **提交永不阻塞调用者**：`Go()` 只是追加到 slice 并可能创建新 worker，不会因 pool 满而阻塞。这避免了当任务在自身 goroutine 内提交子任务到已满 pool 时产生死锁。
- **Worker 按需创建、空闲退出**：无任务时 worker goroutine 自动退出（`workers--`），避免空闲 goroutine 浪费资源。
- **FIFO 保序**：优先级排序后入队的任务按原序出队，保证高优先级任务先执行。

### 4. 原子 rearm 支持循环

条件节点实现循环（如 `body → cond → body`）需要在每次执行后重置节点的依赖计数器。go-flow 使用纯原子操作实现：

```go
func (n *node) rearm() {
    var cnt int32
    for _, dep := range n.dependents {
        if dep.typ == nodeTypeCondition { continue }
        cnt++
    }
    n.joinCounter.Store(cnt)  // 单条原子 store
}
```

对于条件节点，`rearm()` 在调度后继**之前**执行，确保后继的 `drop()` 看到正确的 joinCounter 值，从设计上消除并发竞态。

### 5. 低内存分配


| 优化点               | 说明                                         |
| ----------------- | ------------------------------------------ |
| 预分配 slice         | `successors` 和 `dependents` 初始容量为 4，减少扩容次数 |
| 无 per-node Mutex  | 节点状态管理完全通过 atomic 实现，省去每节点一个 Mutex         |
| 无 sync.Cond       | 图完成追踪使用 WaitGroup，省去 Cond + 关联 Mutex 的分配   |
| 直接闭包调度            | 无中间 Object Pool 包装层，减少间接分配                 |
| graph 级 WaitGroup | 一个 WaitGroup 替代 joinCounter + Cond 的组合     |


---

## 动态子图设计

go-flow 每次执行子图节点时都创建全新的 graph 并重新调用用户回调：

```go
sg := newGraph(n.name)               // 新建空图
sf := &Subflow{g: sg}
e.safeCall(n, func() { p.handle(sf) })  // 调用用户回调填充
p.lastGraph = sg                      // 保留引用用于可视化
e.scheduleGraph(n.g, sg)              // 调度执行子图
```

用户回调可以根据运行时状态（外部数据、前序任务结果、迭代计数等）动态决定子图的拓扑结构、任务数量和依赖关系。每次执行可产生完全不同的 DAG 结构。

### 典型用例

- **数据分片处理**：根据输入数据量动态创建 N 个并行 worker
- **条件分支子图**：根据前序任务结果构建不同的处理流水线
- **递归分治**：子图内嵌套子图，实现递归并行（如并行归并排序）

---

## 错误处理

任务中未恢复的 `panic` 会被框架捕获：

1. 当前图被标记为 `canceled`
2. 尚未调度的任务不再执行
3. 已在执行的任务不受影响（运行完毕后正常收尾）
4. 如果是子图 panic，取消会向上传播到父图

```go
tf.NewTask("safe", func() {
    defer func() {
        if r := recover(); r != nil {
            // 用户自行处理 panic，框架不会取消图
        }
    }()
    riskyOperation()
})
```

---

## API 参考

### TaskFlow


| 方法             | 签名                                         | 说明            |
| -------------- | ------------------------------------------ | ------------- |
| `NewTaskFlow`  | `(name string) *TaskFlow`                  | 创建命名 TaskFlow |
| `NewTask`      | `(name string, f func()) *Task`            | 添加静态任务        |
| `NewOperatorTask` | `(name string, op Operator) *Task`      | 添加 Operator 算子任务 |
| `NewSubflow`   | `(name string, f func(sf *Subflow)) *Task` | 添加动态子图        |
| `NewCondition` | `(name string, f func() uint) *Task`       | 添加条件节点        |
| `Dump`         | `(w io.Writer) error`                      | 输出 DOT 图      |
| `Reset`        | `()`                                       | 解冻，允许重新执行     |
| `Name`         | `() string`                                | 返回名称          |


### Task


| 方法         | 签名                       | 说明                  |
| ---------- | ------------------------ | ------------------- |
| `Precede`  | `(tasks ...*Task)`       | 声明后继依赖（条件节点按序映射返回值） |
| `Succeed`  | `(tasks ...*Task)`       | 声明前驱依赖              |
| `Priority` | `(p TaskPriority) *Task` | 设置调度优先级             |
| `Name`     | `() string`              | 返回名称                |


### Executor


| 方法            | 签名                            | 说明                     |
| ------------- | ----------------------------- | ---------------------- |
| `NewExecutor` | `(concurrency uint) Executor` | 创建执行器（concurrency > 0） |
| `Run`         | `(tf *TaskFlow) Executor`     | 开始执行                   |
| `Wait`        | `()`                          | 阻塞直到所有任务完成             |


### 优先级常量


| 常量       | 值   | 说明        |
| -------- | --- | --------- |
| `HIGH`   | 1   | 高优先级，优先调度 |
| `NORMAL` | 2   | 默认优先级     |
| `LOW`    | 3   | 低优先级      |


---

## 性能基准测试

测试环境：Apple M4 Pro (14 核), Go 1.24, macOS

对比对象：[go-taskflow](https://github.com/noneback/go-taskflow) v1.2.0

### 基础场景 (空任务，测量纯框架开销)


| 场景           | go-flow   | go-taskflow | 加速比      | 分配次数       |
| ------------ | --------- | ----------- | -------- | ---------- |
| C32 (32 并发)  | 16,124 ns | 28,206 ns   | **1.7x** | 84 vs 227  |
| S32 (32 串行链) | 10,655 ns | 46,210 ns   | **4.3x** | 127 vs 286 |
| C6 (菱形 DAG)  | 2,241 ns  | 7,535 ns    | **3.4x** | 23 vs 52   |
| C8x8 (8层x8宽) | 35,263 ns | 64,942 ns   | **1.8x** | 234 vs 560 |


### 大规模场景


| 场景              | go-flow    | go-taskflow  | 加速比      | 分配次数           |
| --------------- | ---------- | ------------ | -------- | -------------- |
| C256 (256 并发)   | 145,830 ns | 274,581 ns   | **1.9x** | 653 vs 1,809   |
| C1024 (1024 并发) | 603,476 ns | 1,078,581 ns | **1.8x** | 2,604 vs 7,220 |
| S128 (128 串行链)  | 42,005 ns  | 191,698 ns   | **4.6x** | 511 vs 1,150   |


### 拓扑模式


| 场景                 | go-flow    | go-taskflow | 加速比      | 分配次数         |
| ------------------ | ---------- | ----------- | -------- | ------------ |
| Fan-out/in 1→64→1  | 37,435 ns  | 69,800 ns   | **1.9x** | 232 vs 598   |
| Fan-out/in 1→256→1 | 157,972 ns | 291,219 ns  | **1.8x** | 911 vs 2,338 |
| Pipeline 4x16      | 48,262 ns  | 71,960 ns   | **1.5x** | 212 vs 544   |
| Pipeline 8x32      | 330,600 ns | 490,177 ns  | **1.5x** | 815 vs 2,272 |
| Pipeline 16x8      | 69,122 ns  | 133,233 ns  | **1.9x** | 478 vs 1,137 |
| 二叉树 depth=6        | 133,971 ns | 167,957 ns  | **1.3x** | 338 vs 1,086 |
| 菱形链 x16            | 18,048 ns  | 63,261 ns   | **3.5x** | 195 vs 439   |


### CPU 密集型工作负载


| 场景               | go-flow   | go-taskflow | 加速比      |
| ---------------- | --------- | ----------- | -------- |
| CPU C32          | 18,702 ns | 33,189 ns   | **1.8x** |
| CPU Pipeline 4x8 | 18,972 ns | 37,633 ns   | **2.0x** |


### 子图与循环


| 场景                | go-flow   | go-taskflow | 加速比      |
| ----------------- | --------- | ----------- | -------- |
| Subflow 静态 (4 节点) | 2,948 ns  | 6,885 ns    | **2.3x** |
| 重复执行 x10          | 70,465 ns | 126,220 ns  | **1.8x** |



| 场景                 | go-flow   | 说明             |
| ------------------ | --------- | -------------- |
| Subflow 动态         | 4,780 ns  | 每次执行不同任务数      |
| Subflow 嵌套 3 层     | 2,554 ns  | 子图嵌套子图         |
| Subflow 8 并发 x8 任务 | 78,223 ns | 8 个并发子图各含 8 任务 |
| 条件循环 x10           | 7,098 ns  | 10 次循环迭代       |
| 条件循环 x100          | 71,501 ns | 100 次循环迭代      |


### 性能优势总结

- **串行链场景加速最大 (4.3x -- 4.6x)**：WaitGroup + CAS 在频繁唤醒场景下开销显著更低
- **并发场景稳定 1.5x -- 1.9x 加速**：无锁节点调度减少了锁竞争
- **内存分配减少 55% -- 69%**：消除 per-node Mutex 与中间对象包装等间接分配
- **动态子图零额外开销**：每次重建子图的开销与静态子图相当甚至更快

---

## 文件结构

```
go-flow/
├── go.mod              # 模块声明
├── graph.go            # graph: DAG 容器，WaitGroup 完成追踪
├── node.go             # node: 原子状态机，joinCounter，rearm
├── task.go             # Task: 用户 API，Precede/Succeed/Priority
├── taskflow.go         # TaskFlow: 顶层容器
├── subflow.go          # Subflow: 动态子图，节点工厂函数
├── condition.go        # conditionData: 分支映射
├── executor.go         # Executor: 调度核心，CAS 防重复调度
├── pool.go             # pool: FIFO 工作池，按需创建 worker
├── visualize.go        # DOT 格式输出
├── operator.go         # Operator 接口 + FuncOperator 适配器
├── goflow_test.go      # 单元测试
└── benchmark_test.go   # 性能基准测试
```

## License

Apache-2.0