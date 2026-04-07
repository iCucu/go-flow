# go-flow 设计原理

## 一、核心思想

go-flow 的目标是以最低的调度开销执行用户定义的 DAG（有向无环图）。整体思路可以用一句话概括：

> **用原子操作替代互斥锁，用 WaitGroup 替代条件变量循环，用 FIFO 队列保证优先级。**

---

## 二、数据模型

### 2.1 三层结构

```
用户看到的          内部表示            执行引擎
─────────         ─────────          ─────────
TaskFlow    ──►   graph              Executor
Task        ──►   node               pool
Subflow     ──►   graph (子图)
Operator    ──►   staticData.op
```

用户通过 `TaskFlow` / `Task` 构建 DAG，框架内部转化为 `graph` + `node` 的有向图，再由 `Executor` 调度到 `pool` 中执行。

### 2.2 node 的双重身份

每个 `node` 同时承担两个角色：

1. **图结构节点** -- 通过 `successors` / `dependents` 两个切片维护前驱和后继关系
2. **运行时调度单元** -- 通过 `state` (原子状态机) 和 `joinCounter` (原子依赖计数) 管理执行时序

```
node {
    successors  ──►  图结构：我完成后谁可以运行
    dependents  ──►  图结构：谁完成了我才能运行
    state       ──►  调度：idle / waiting / running / finished
    joinCounter ──►  调度：还有几个前驱没完成
}
```

---

## 三、调度原理

### 3.1 依赖计数 (joinCounter)

核心问题：**一个节点什么时候可以开始执行？**

答案：当它的所有非条件前驱都完成时。框架用 `joinCounter` 跟踪这一状态：

```
graph.setup()
  │
  ├── 对每个节点 n：
  │     n.joinCounter = 非条件前驱的数量
  │     例：A→C, B→C 则 C.joinCounter = 2
  │
  └── 入口节点 (joinCounter == 0, 无前驱) 加入 entries

节点完成时 (drop):
  对每个后继 succ：
    succ.joinCounter -= 1    // 原子操作
    如果 succ.joinCounter == 0 → 可调度
```

### 3.2 CAS 防重复调度

当多个前驱并发完成时，多个 goroutine 可能同时发现某个后继的 `joinCounter == 0`。如果不加控制，该后继会被调度多次。

go-flow 用一条原子 CAS 解决：

```
if succ.joinCounter == 0 && CAS(succ.state, idle → waiting) {
    // 只有第一个 CAS 成功的 goroutine 负责调度
    schedule(succ)
}
```

两个 goroutine 同时尝试 CAS，只有一个能成功（idle → waiting），另一个看到的已经是 waiting 状态，直接跳过。无需互斥锁。

### 3.3 完整调度流程

```
1. graph.setup()        ← 初始化所有 joinCounter，找出入口节点
2. schedule(entries)    ← 入口节点提交到 pool

3. pool 中的 worker 取出节点执行：
   ┌──────────────────────────────────────────┐
   │  执行 op.Compute()                        │
   │                                          │
   │  drop()                                  │
   │    对每个后继：joinCounter -= 1             │
   │                                          │
   │  scheduleSuccessors()                    │
   │    对每个后继：                             │
   │      如果 joinCounter==0 且 CAS(idle→waiting)│
   │        → schedule(succ)                  │
   │                                          │
   │  rearm()                                 │
   │    重置自身 joinCounter（为循环做准备）       │
   │                                          │
   │  state ← idle                            │
   │  wg.Done()                               │
   └──────────────────────────────────────────┘

4. graph.wg.Wait()      ← 等待所有节点完成
```

---

## 四、三种节点类型

### 4.1 Static / Operator

最基础的节点。用户提供 `func()` 或 `Operator.Compute()`。调度流程如上。

```
NewTask("name", func() { ... })         → FuncOperator → staticData
NewOperatorTask("name", myOperator)      → staticData
```

两者内部完全一致，都是 `staticData{op: Operator}`。

### 4.2 Subflow (动态子图)

Subflow 的关键在于**每次执行都创建新的子图**：

```
invokeSubflow(n):
  1. sg = newGraph()           ← 空白子图
  2. sf = &Subflow{g: sg}
  3. user_callback(sf)          ← 用户填充子图（可根据运行时状态决定拓扑）
  4. scheduleGraph(parent, sg)  ← 递归调度子图，阻塞直到子图完成
  5. 子图完成后，继续执行外层后继
```

这意味着同一个 Subflow 节点在不同次执行中可以产生完全不同的 DAG 结构。

### 4.3 Condition (条件分支)

Condition 节点的特殊之处在于它**只调度一个后继**，而不是像 Static 节点那样触发所有就绪后继：

```
invokeCondition(n):
  1. choice = predicate()       ← 用户函数返回 0, 1, 2...
  2. rearm()                    ← 先重置自身 joinCounter
  3. state ← idle               ← 先恢复 idle
  4. schedule(mapper[choice])   ← 只调度选中的那个后继
```

**为什么 rearm 在 schedule 之前？**

考虑循环场景 `body → cond → body`：

```
body 完成 → drop → cond.joinCounter = 0 → schedule(cond)
cond 完成 → 选择 body → schedule(body)
body 再次完成 → drop → 需要递减 cond.joinCounter

如果 cond 没有 rearm，此时 cond.joinCounter 已经是 0，
body.drop() 会让它变成 -1，导致 cond 永远不会被重新调度。

所以 cond 必须在 schedule(body) 之前 rearm()，
把 joinCounter 恢复为 1，这样 body.drop() 后 joinCounter 回到 0，循环继续。
```

---

## 五、Worker Pool

### 5.1 为什么不能用简单的信号量池

最简单的 pool 设计是「先获取信号量，再启动 goroutine」：

```go
sem <- struct{}{}    // 阻塞直到有空位
go func() {
    defer func() { <-sem }()
    f()
}()
```

**问题**：当一个任务在执行中需要调度新任务（比如 `scheduleSuccessors`），而 pool 已满时，当前 goroutine 会阻塞在 `sem <-` 上，但它自己又占着一个信号量位——**死锁**。

### 5.2 go-flow 的 FIFO 池

```go
func (p *pool) Go(f func()) {
    p.mu.Lock()
    p.queue = append(p.queue, f)   // 入队，永不阻塞
    if p.workers < p.cap {
        p.workers++
        p.mu.Unlock()
        go p.run()                 // 按需创建 worker
    } else {
        p.mu.Unlock()
    }
}
```

- `Go()` 只是把任务追加到 slice，调用者立即返回
- Worker 从 queue 头部取任务执行，队列空时 worker 退出
- FIFO 顺序保证：如果 schedule 时按优先级排序入队，出队顺序就是优先级顺序

---

## 六、图完成追踪

每个 `graph` 内嵌一个 `sync.WaitGroup`：

```
schedule(n)  →  g.wg.Add(1)
nodeFinished →  g.wg.Done()

scheduleGraph 的末尾：g.wg.Wait()   ← 等待此图内所有节点完成
```

这比 `sync.Cond` 循环简洁得多。Cond 需要反复 Lock → 检查条件 → Wait → Unlock，而 WaitGroup 只需一次 `Wait()` 调用。

Executor 自身也维护一个 `wg`，用于跨图等待（包含所有子图的任务）：

```
executor.Run(tf)   → 触发顶层图
executor.Wait()    → 等待所有图所有任务
```

---

## 七、Panic 处理

每个任务的执行被 `safeCall` 包裹：

```go
defer func() {
    if r := recover(); r != nil {
        n.g.canceled.Store(true)   // 标记此图已取消
        log.Printf(...)
    }
}()
f()
```

一旦某个任务 panic：

1. 当前图的 `canceled` 标志被设为 true
2. `schedule()` 检查 `canceled` 后跳过所有新调度
3. 已在运行的任务不受影响（运行完毕后正常走 `nodeFinished` 流程）
4. 子图的 `canceled` 会在 `scheduleGraph` 返回后传播给父图

用户也可以在任务内部自行 `recover()`，此时框架不会感知到 panic，图不会被取消。

---

## 八、一个完整的例子

```
DAG: A → B, A → C, B → D, C → D

setup 后：
  A.joinCounter = 0 (入口)
  B.joinCounter = 1 (依赖 A)
  C.joinCounter = 1 (依赖 A)
  D.joinCounter = 2 (依赖 B 和 C)

执行：
  1. schedule(A)
  2. A 执行完毕
     drop: B.jc 1→0, C.jc 1→0
     scheduleSuccessors:
       B: jc==0, CAS(idle→waiting) ✓ → schedule(B)
       C: jc==0, CAS(idle→waiting) ✓ → schedule(C)
  3. B 和 C 并行执行
     B 完成: drop → D.jc 2→1
       D: jc==1, 不调度
     C 完成: drop → D.jc 1→0
       D: jc==0, CAS(idle→waiting) ✓ → schedule(D)
  4. D 执行完毕
     drop: 无后继
     graph.wg 归零 → Wait() 返回
```

---

## 九、与传统方案的对比

| 维度 | 传统方案 (sync.Mutex + sync.Cond) | go-flow (atomic + WaitGroup) |
|---|---|---|
| 节点状态转换 | Lock → 检查 → 修改 → Unlock | 单条 CAS 指令 |
| 防重复调度 | Lock 保护的 if-check | CAS 天然互斥 |
| 图完成等待 | Cond.Wait() 循环 + Signal | WaitGroup.Wait() 一次调用 |
| 循环支持 | Mutex 保护的 setup() | 原子 rearm() |
| Pool 提交 | 可能阻塞（信号量/有界 channel） | 永不阻塞（FIFO slice） |

每一项替换都减少了锁的使用，减少了上下文切换，从而在基准测试中取得 1.2x -- 5x 的加速。
