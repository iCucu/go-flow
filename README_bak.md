# go-flow

高性能 Go DAG 任务并行执行框架，灵感来源于 [go-taskflow](https://github.com/noneback/go-taskflow)。

go-flow 在兼容 go-taskflow 编程模型的基础上，实现了**动态子图**与多项性能优化，在全部基准场景下均取得 **1.3x -- 4.6x** 的加速，内存分配减少 **55% -- 69%**。

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

| 类型 | 用途 | 调度行为 |
|---|---|---|
| **Static** | 普通函数 `func()` | 前驱全部完成后调度，完成后触发后继 |
| **Subflow** | 动态子图 `func(sf *Subflow)` | 每次执行创建新 graph，回调重新构建拓扑 |
| **Condition** | 分支 `func() uint` | 返回值选择唯一后继，支持循环 |

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

### 与 go-taskflow 的关键差异

#### 1. 无锁节点状态转换

go-taskflow 在每个节点上使用 `sync.Mutex` 保护状态检查与转换：

```go
// go-taskflow: 需要 lock/unlock
n.mu.Lock()
if n.recyclable() && n.state.Load() == kNodeStateIdle {
    n.state.Store(kNodeStateWaiting)
    candidate = append(candidate, n)
}
n.mu.Unlock()
```

go-flow 使用 `atomic.CompareAndSwap` 实现无锁调度：

```go
// go-flow: 单条原子指令，无需互斥锁
if succ.recyclable() && succ.state.CompareAndSwap(int32(nodeIdle), int32(nodeWaiting)) {
    candidates = append(candidates, succ)
}
```

CAS 操作将「检查状态 + 修改状态」合并为单条原子指令，消除了互斥锁的上下文切换开销，同时天然防止多个前驱并发完成时对同一后继的重复调度。

#### 2. WaitGroup 替代 sync.Cond

go-taskflow 使用 `sync.Cond` 循环等待图完成：

```go
// go-taskflow: invokeGraph 中的等待循环
for {
    g.scheCond.L.Lock()
    e.mu.Lock()
    for !g.recyclable() && e.wq.Len() == 0 && !g.canceled.Load() {
        e.mu.Unlock()
        g.scheCond.Wait()   // 阻塞等待信号
        e.mu.Lock()
    }
    g.scheCond.L.Unlock()
    // ... 处理逻辑 ...
}
```

这个循环每次唤醒都需要 Lock → Wait → Signal → Unlock，开销显著。

go-flow 直接使用 `sync.WaitGroup`：

```go
// go-flow: 一行搞定
g.wg.Wait()
```

每个节点被调度时 `wg.Add(1)`，完成时 `wg.Done()`。WaitGroup 内部使用原子操作 + 信号量，比 Cond 循环更轻量。

#### 3. FIFO 工作池 (非阻塞提交)

go-taskflow 的 `Copool` 使用单一互斥锁保护的全局队列，所有工作线程竞争同一把锁：

```go
// go-taskflow Copool: 全局锁竞争
cp.mu.Lock()
cp.taskQ.Put(task)
if cp.coworker < cp.cap {
    cp.coworker++
    cp.mu.Unlock()
    go worker()
} else {
    cp.mu.Unlock()
}
```

go-flow 的 pool 同样使用互斥锁 + FIFO 队列，但关键区别在于：

- **提交永不阻塞调用者**：`Go()` 只是追加到 slice 并可能创建新 worker，不会因 pool 满而阻塞
- **Worker 按需创建、空闲退出**：避免了固定 worker 数的资源浪费
- **FIFO 保序**：优先级排序后入队的任务按原序出队，保证高优先级任务先执行

这避免了 go-taskflow 中当任务需要提交子任务到已满 pool 时的潜在死锁。

#### 4. 节点重臂 (rearm) 支持循环

go-taskflow 在 `sche_successors` 末尾调用 `node.setup()` 重置节点，但 setup 内部使用 `sync.Mutex`。go-flow 的 `rearm()` 纯原子操作：

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

对于条件节点，`rearm()` 在调度后继**之前**执行，确保后继的 `drop()` 看到正确的 joinCounter 值，消除了并发竞态。

#### 5. 更少的内存分配

| 优化点 | 说明 |
|---|---|
| 预分配 slice | `successors` 和 `dependents` 初始容量为 4，减少扩容 |
| 无 `sync.Cond` | 省去 Cond + 关联 Mutex 的分配 |
| 无 per-node Mutex | 省去每个节点一个 `sync.Mutex` 的分配 |
| 无 Object Pool 间接层 | go-taskflow 使用 ObjectPool 包装 cotask，go-flow 直接闭包 |
| graph 级 WaitGroup | 一个 WaitGroup 替代 joinCounter + Cond 的组合 |

---

## 动态子图设计

### 与 go-taskflow 的对比

go-taskflow 的 subflow 使用 `instantiated` 标志位：

```go
// go-taskflow: 子图只构建一次
if !p.g.instantiated {
    p.handle(p)              // 只在第一次执行时调用
}
p.g.instantiated = true      // 后续执行复用同一子图
```

go-flow 每次执行都创建全新的 graph 并重新调用用户回调：

```go
// go-flow: 每次执行都重新构建
sg := newGraph(n.name)               // 新建空图
sf := &Subflow{g: sg}
e.safeCall(n, func() { p.handle(sf) })  // 调用用户回调填充
p.lastGraph = sg                      // 保留引用用于可视化
e.scheduleGraph(n.g, sg)              // 调度执行子图
```

这意味着用户回调可以根据运行时状态（外部数据、前序任务结果等）动态决定子图的拓扑结构、任务数量和依赖关系。

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

| 方法 | 签名 | 说明 |
|---|---|---|
| `NewTaskFlow` | `(name string) *TaskFlow` | 创建命名 TaskFlow |
| `NewTask` | `(name string, f func()) *Task` | 添加静态任务 |
| `NewSubflow` | `(name string, f func(sf *Subflow)) *Task` | 添加动态子图 |
| `NewCondition` | `(name string, f func() uint) *Task` | 添加条件节点 |
| `Dump` | `(w io.Writer) error` | 输出 DOT 图 |
| `Reset` | `()` | 解冻，允许重新执行 |
| `Name` | `() string` | 返回名称 |

### Task

| 方法 | 签名 | 说明 |
|---|---|---|
| `Precede` | `(tasks ...*Task)` | 声明后继依赖（条件节点按序映射返回值） |
| `Succeed` | `(tasks ...*Task)` | 声明前驱依赖 |
| `Priority` | `(p TaskPriority) *Task` | 设置调度优先级 |
| `Name` | `() string` | 返回名称 |

### Executor

| 方法 | 签名 | 说明 |
|---|---|---|
| `NewExecutor` | `(concurrency uint) Executor` | 创建执行器（concurrency > 0） |
| `Run` | `(tf *TaskFlow) Executor` | 开始执行 |
| `Wait` | `()` | 阻塞直到所有任务完成 |

### 优先级常量

| 常量 | 值 | 说明 |
|---|---|---|
| `HIGH` | 1 | 高优先级，优先调度 |
| `NORMAL` | 2 | 默认优先级 |
| `LOW` | 3 | 低优先级 |

---

## 性能基准测试

测试环境：Apple M4 Pro (14 核), Go 1.24, macOS

### 基础场景 (空任务，测量纯框架开销)

| 场景 | go-flow | go-taskflow | 加速比 | 分配次数 |
|---|---|---|---|---|
| C32 (32 并发) | 16,124 ns | 28,206 ns | **1.7x** | 84 vs 227 |
| S32 (32 串行链) | 10,655 ns | 46,210 ns | **4.3x** | 127 vs 286 |
| C6 (菱形 DAG) | 2,241 ns | 7,535 ns | **3.4x** | 23 vs 52 |
| C8x8 (8层x8宽) | 35,263 ns | 64,942 ns | **1.8x** | 234 vs 560 |

### 大规模场景

| 场景 | go-flow | go-taskflow | 加速比 | 分配次数 |
|---|---|---|---|---|
| C256 (256 并发) | 145,830 ns | 274,581 ns | **1.9x** | 653 vs 1,809 |
| C1024 (1024 并发) | 603,476 ns | 1,078,581 ns | **1.8x** | 2,604 vs 7,220 |
| S128 (128 串行链) | 42,005 ns | 191,698 ns | **4.6x** | 511 vs 1,150 |

### 拓扑模式

| 场景 | go-flow | go-taskflow | 加速比 | 分配次数 |
|---|---|---|---|---|
| Fan-out/in 1→64→1 | 37,435 ns | 69,800 ns | **1.9x** | 232 vs 598 |
| Fan-out/in 1→256→1 | 157,972 ns | 291,219 ns | **1.8x** | 911 vs 2,338 |
| Pipeline 4x16 | 48,262 ns | 71,960 ns | **1.5x** | 212 vs 544 |
| Pipeline 8x32 | 330,600 ns | 490,177 ns | **1.5x** | 815 vs 2,272 |
| Pipeline 16x8 | 69,122 ns | 133,233 ns | **1.9x** | 478 vs 1,137 |
| 二叉树 depth=6 | 133,971 ns | 167,957 ns | **1.3x** | 338 vs 1,086 |
| 菱形链 x16 | 18,048 ns | 63,261 ns | **3.5x** | 195 vs 439 |

### CPU 密集型工作负载

| 场景 | go-flow | go-taskflow | 加速比 |
|---|---|---|---|
| CPU C32 | 18,702 ns | 33,189 ns | **1.8x** |
| CPU Pipeline 4x8 | 18,972 ns | 37,633 ns | **2.0x** |

### 子图与循环 (go-flow 独有)

| 场景 | go-flow | 说明 |
|---|---|---|
| Subflow 静态 | 2,948 ns | 4 节点子图 (go-taskflow: 6,885 ns, **2.3x**) |
| Subflow 动态 | 4,780 ns | 每次执行不同任务数 |
| Subflow 嵌套 3 层 | 2,554 ns | 子图嵌套子图 |
| Subflow 8 并发 x8 任务 | 78,223 ns | 8 个并发子图各含 8 任务 |
| 条件循环 x10 | 7,098 ns | 10 次循环迭代 |
| 条件循环 x100 | 71,501 ns | 100 次循环迭代 |
| 重复执行 x10 | 70,465 ns | 同一 taskflow 执行 10 次 (go-taskflow: 126,220 ns, **1.8x**) |

### 性能优势总结

- **串行链场景加速最大 (4.3x -- 4.6x)**：`sync.WaitGroup` + CAS 比 `sync.Cond` 循环在频繁唤醒场景下开销显著更低
- **并发场景稳定 1.5x -- 1.9x 加速**：无锁节点调度减少了锁竞争
- **内存分配减少 55% -- 69%**：消除 per-node Mutex、ObjectPool 包装等间接分配
- **动态子图零额外开销**：每次重建子图的开销与 go-taskflow 的静态子图相当甚至更快

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
├── goflow_test.go      # 19 项单元测试 (通过 -race)
└── benchmark_test.go   # 性能基准 + go-taskflow 对比
```

## License

Apache-2.0
