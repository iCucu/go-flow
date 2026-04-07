# go-flow 测试与性能报告

生成时间：2026-04-07

测试环境：

- OS: macOS (darwin/arm64)
- CPU: Apple M4 Pro (14 核)
- Go: 1.24.4
- 对比版本：go-taskflow v1.2.0

---

## 一、单元测试结果

共 **34 项**测试，全部 **PASS**，启用 `-race` 竞态检测。

```
go test -v -race -count=1 ./...
```

| # | 测试用例 | 分类 | 结果 |
|---|---|---|---|
| 1 | TestLinearChain | DAG 拓扑 | PASS |
| 2 | TestDiamondDAG | DAG 拓扑 | PASS |
| 3 | TestFullyParallel32 | DAG 拓扑 | PASS |
| 4 | TestStaticSubflow | 子图 | PASS |
| 5 | TestDynamicSubflow | 子图 | PASS |
| 6 | TestNestedSubflow | 子图 | PASS |
| 7 | TestConditionBasicBranch | 条件/循环 | PASS |
| 8 | TestConditionLoop | 条件/循环 | PASS |
| 9 | TestPriorityScheduling | 调度 | PASS |
| 10 | TestEmptyTaskFlow | 生命周期 | PASS |
| 11 | TestSingleNode | 生命周期 | PASS |
| 12 | TestPanicCancelsGraph | 容错 | PASS |
| 13 | TestDumpDOT | 工具 | PASS |
| 14 | TestTaskName | 工具 | PASS |
| 15 | TestConcurrencyStress | 调度 | PASS |
| 16 | TestMultipleRunsSameTaskflow | 生命周期 | PASS |
| 17 | TestConditionWithSubflow | 条件/子图 | PASS |
| 18 | TestFrozenTaskflowPanics | 生命周期 | PASS |
| 19 | TestWideDAG | DAG 拓扑 | PASS |
| 20 | TestMultiSourceMultiSink | DAG 拓扑 (复杂) | PASS |
| 21 | TestWShapedDAG | DAG 拓扑 (复杂) | PASS |
| 22 | TestHourglassDAG | DAG 拓扑 (复杂) | PASS |
| 23 | TestLongDiamondChain | DAG 拓扑 (复杂) | PASS |
| 24 | TestLayeredPipelineWithCrossDeps | DAG 拓扑 (复杂) | PASS |
| 25 | TestIrregularDAG | DAG 拓扑 (复杂) | PASS |
| 26 | TestParallelChainsConverge | DAG 拓扑 (复杂) | PASS |
| 27 | TestComplexMultiPathDAG | DAG 拓扑 (复杂) | PASS |
| 28 | TestOperatorBasic | Operator | PASS |
| 29 | TestOperatorDAGOrder | Operator | PASS |
| 30 | TestOperatorWithFuncMixed | Operator | PASS |
| 31 | TestFuncOperatorAdapter | Operator | PASS |
| 32 | TestOperatorInSubflow | Operator | PASS |
| 33 | TestOperatorStateful | Operator | PASS |
| 34 | TestOperatorPriority | Operator | PASS |

竞态检测：**无数据竞争**

---

## 二、性能基准对比

所有 benchmark 运行 3 次取中位数，使用 `-benchmem` 记录内存分配。

### 2.1 基础场景 (空任务，纯框架开销)

| 场景 | go-flow (ns/op) | go-taskflow (ns/op) | 加速比 | go-flow (allocs) | go-taskflow (allocs) | 分配减少 |
|---|---|---|---|---|---|---|
| C32 (32 并发) | 15,163 | 27,676 | **1.83x** | 84 | 227 | 63% |
| S32 (32 串行链) | 9,906 | 49,024 | **4.95x** | 127 | 286 | 56% |
| C6 (菱形 DAG) | 2,006 | 7,634 | **3.81x** | 23 | 52 | 56% |
| C8x8 (8层x8宽) | 33,084 | 66,276 | **2.00x** | 234 | 560 | 58% |

### 2.2 大规模场景

| 场景 | go-flow (ns/op) | go-taskflow (ns/op) | 加速比 | go-flow (allocs) | go-taskflow (allocs) | 分配减少 |
|---|---|---|---|---|---|---|
| C256 (256 并发) | 149,541 | 276,276 | **1.85x** | 654 | 1,811 | 64% |
| C1024 (1024 并发) | 615,354 | 1,107,237 | **1.80x** | 2,615 | 7,227 | 64% |
| S128 (128 串行链) | 40,996 | 189,521 | **4.62x** | 511 | 1,150 | 56% |

### 2.3 拓扑模式

| 场景 | go-flow (ns/op) | go-taskflow (ns/op) | 加速比 | go-flow (allocs) | go-taskflow (allocs) | 分配减少 |
|---|---|---|---|---|---|---|
| Fan-out/in 1→64→1 | 35,877 | 70,237 | **1.96x** | 232 | 599 | 61% |
| Fan-out/in 1→256→1 | 157,583 | 300,079 | **1.90x** | 912 | 2,340 | 61% |
| Pipeline 4x16 | 48,070 | 73,176 | **1.52x** | 212 | 545 | 61% |
| Pipeline 8x32 | 338,346 | 483,679 | **1.43x** | 814 | 2,274 | 64% |
| Pipeline 16x8 | 68,289 | 130,638 | **1.91x** | 478 | 1,137 | 58% |
| 二叉树 depth=6 (127 节点) | 140,944 | 168,253 | **1.19x** | 340 | 1,087 | 69% |
| 菱形链 x16 | 17,203 | 62,843 | **3.65x** | 195 | 439 | 56% |

### 2.4 CPU 密集型工作负载

| 场景 | go-flow (ns/op) | go-taskflow (ns/op) | 加速比 | go-flow (allocs) | go-taskflow (allocs) |
|---|---|---|---|---|---|
| CPU C32 | 20,532 | 31,168 | **1.52x** | 80 | 227 |
| CPU Pipeline 4x8 | 19,296 | 37,671 | **1.95x** | 109 | 272 |

### 2.5 子图

| 场景 | go-flow (ns/op) | go-taskflow (ns/op) | 加速比 | go-flow (allocs) | go-taskflow (allocs) |
|---|---|---|---|---|---|
| Subflow 静态 (4 节点) | 2,975 | 6,610 | **2.22x** | 42 | 41 |
| 重复执行 x10 | 70,903 | 124,082 | **1.75x** | 436 | 1,121 |

### 2.6 go-flow 独有场景

| 场景 | go-flow (ns/op) | allocs/op | 说明 |
|---|---|---|---|
| Subflow 动态 (1-10 任务) | 4,824 | 54 | 每次执行不同任务数 |
| Subflow 嵌套 3 层 | 2,521 | 39 | 子图嵌套子图嵌套子图 |
| Subflow 8 并发 x8 任务 | 80,856 | 580 | 8 个并发子图各含 8 任务 |
| 条件循环 x10 | 7,084 | 77 | 10 次 body→cond 循环 |
| 条件循环 x100 | 67,963 | 707 | 100 次 body→cond 循环 |

---

## 三、综合分析

### 延迟 (ns/op)

| 场景类别 | 加速比范围 | 典型值 |
|---|---|---|
| 串行链 | 4.62x -- 4.95x | **~4.8x** |
| 菱形/链式 DAG | 3.65x -- 3.81x | **~3.7x** |
| 子图 | 1.75x -- 2.22x | **~2.0x** |
| 全连接/流水线 | 1.43x -- 2.00x | **~1.7x** |
| 大规模并发 | 1.80x -- 1.85x | **~1.8x** |
| CPU 密集 | 1.52x -- 1.95x | **~1.7x** |
| 二叉树 (高扇出) | 1.19x | **1.2x** |

### 内存分配 (allocs/op)

| 场景类别 | 分配减少比例 |
|---|---|
| 基础场景 (C6/C32/S32/C8x8) | 56% -- 63% |
| 大规模 (C256/C1024/S128) | 56% -- 64% |
| 拓扑模式 | 56% -- 69% |

### 关键结论

1. **串行链场景优势最大**（~5x）：`sync.WaitGroup` + CAS 在高频唤醒路径下远优于 `sync.Cond` 循环。

2. **大规模并发场景稳定 1.8x**：无锁 CAS 调度在高并发下减少了锁竞争，优势随节点数增长保持稳定。

3. **内存分配一致减少 56-69%**：消除 per-node Mutex、sync.Cond、ObjectPool 包装层带来的固定分配。

4. **二叉树场景加速相对较小 (1.2x)**：高扇出 + 浅依赖链使调度开销占比低，框架差异不显著。

5. **动态子图无性能代价**：go-flow 每次重建子图（2,975 ns）反而快于 go-taskflow 的静态子图（6,610 ns），因为省去了 instantiated 状态检查和 sync.Cond 同步。

---

## 四、原始 benchmark 输出

```
goos: darwin
goarch: arm64
pkg: github.com/zhiyidu/go-flow
cpu: Apple M4 Pro

BenchmarkGoFlow_C32-14                           74852     14994 ns/op     1663 B/op      84 allocs/op
BenchmarkGoFlow_C32-14                           73917     15163 ns/op     1664 B/op      84 allocs/op
BenchmarkGoFlow_C32-14                           82021     16529 ns/op     1675 B/op      84 allocs/op
BenchmarkGoFlow_S32-14                          121410      9906 ns/op     1784 B/op     127 allocs/op
BenchmarkGoFlow_S32-14                          124244      9755 ns/op     1784 B/op     127 allocs/op
BenchmarkGoFlow_S32-14                          120871     10025 ns/op     1784 B/op     127 allocs/op
BenchmarkGoFlow_C6-14                           554724      2090 ns/op      353 B/op      23 allocs/op
BenchmarkGoFlow_C6-14                           591176      2006 ns/op      353 B/op      23 allocs/op
BenchmarkGoFlow_C6-14                           611338      1985 ns/op      353 B/op      23 allocs/op
BenchmarkGoFlow_C8x8-14                          37810     31491 ns/op     6893 B/op     235 allocs/op
BenchmarkGoFlow_C8x8-14                          33502     33084 ns/op     6897 B/op     234 allocs/op
BenchmarkGoFlow_C8x8-14                          36232     33822 ns/op     6899 B/op     234 allocs/op
BenchmarkGoFlow_C256-14                           7460    149833 ns/op    13427 B/op     656 allocs/op
BenchmarkGoFlow_C256-14                           8386    149541 ns/op    13440 B/op     654 allocs/op
BenchmarkGoFlow_C256-14                           8066    146721 ns/op    13422 B/op     655 allocs/op
BenchmarkGoFlow_C1024-14                          2028    608514 ns/op    53753 B/op    2614 allocs/op
BenchmarkGoFlow_C1024-14                          2008    615354 ns/op    53760 B/op    2615 allocs/op
BenchmarkGoFlow_C1024-14                          1987    616267 ns/op    53813 B/op    2610 allocs/op
BenchmarkGoFlow_S128-14                          29211     41726 ns/op     7162 B/op     511 allocs/op
BenchmarkGoFlow_S128-14                          29289     40846 ns/op     7162 B/op     511 allocs/op
BenchmarkGoFlow_S128-14                          29619     40996 ns/op     7162 B/op     511 allocs/op
BenchmarkGoFlow_FanOutFanIn_64-14                34516     35877 ns/op     4505 B/op     232 allocs/op
BenchmarkGoFlow_FanOutFanIn_64-14                34158     36023 ns/op     4506 B/op     232 allocs/op
BenchmarkGoFlow_FanOutFanIn_64-14                34225     35473 ns/op     4503 B/op     232 allocs/op
BenchmarkGoFlow_FanOutFanIn_256-14                8215    156316 ns/op    17917 B/op     913 allocs/op
BenchmarkGoFlow_FanOutFanIn_256-14                7848    159931 ns/op    17933 B/op     912 allocs/op
BenchmarkGoFlow_FanOutFanIn_256-14                7346    157583 ns/op    17922 B/op     912 allocs/op
BenchmarkGoFlow_Pipeline_4x16-14                 25430     48070 ns/op     9568 B/op     212 allocs/op
BenchmarkGoFlow_Pipeline_4x16-14                 25081     48777 ns/op     9569 B/op     212 allocs/op
BenchmarkGoFlow_Pipeline_4x16-14                 24067     48735 ns/op     9570 B/op     212 allocs/op
BenchmarkGoFlow_Pipeline_8x32-14                  3466    338346 ns/op    71308 B/op     814 allocs/op
BenchmarkGoFlow_Pipeline_8x32-14                  3657    338337 ns/op    71394 B/op     815 allocs/op
BenchmarkGoFlow_Pipeline_8x32-14                  3600    339191 ns/op    71303 B/op     813 allocs/op
BenchmarkGoFlow_Pipeline_16x8-14                 17258     68924 ns/op    14289 B/op     478 allocs/op
BenchmarkGoFlow_Pipeline_16x8-14                 17678     68071 ns/op    14286 B/op     478 allocs/op
BenchmarkGoFlow_Pipeline_16x8-14                 17565     68289 ns/op    14285 B/op     478 allocs/op
BenchmarkGoFlow_BinaryTree_depth6-14              8865    140944 ns/op     8096 B/op     340 allocs/op
BenchmarkGoFlow_BinaryTree_depth6-14              8486    139965 ns/op     8098 B/op     340 allocs/op
BenchmarkGoFlow_BinaryTree_depth6-14              8761    143038 ns/op     8103 B/op     340 allocs/op
BenchmarkGoFlow_DiamondChain_16-14               67404     17203 ns/op     2941 B/op     195 allocs/op
BenchmarkGoFlow_DiamondChain_16-14               69960     17202 ns/op     2940 B/op     195 allocs/op
BenchmarkGoFlow_DiamondChain_16-14               70188     17261 ns/op     2940 B/op     195 allocs/op
BenchmarkGoFlow_Subflow_Static-14               412258      2930 ns/op     1279 B/op      42 allocs/op
BenchmarkGoFlow_Subflow_Static-14               410125      3027 ns/op     1279 B/op      42 allocs/op
BenchmarkGoFlow_Subflow_Static-14               406599      2975 ns/op     1279 B/op      42 allocs/op
BenchmarkGoFlow_Subflow_Dynamic_10-14           236071      4824 ns/op     1756 B/op      54 allocs/op
BenchmarkGoFlow_Subflow_Dynamic_10-14           248904      4779 ns/op     1756 B/op      54 allocs/op
BenchmarkGoFlow_Subflow_Dynamic_10-14           250885      4855 ns/op     1756 B/op      54 allocs/op
BenchmarkGoFlow_Subflow_Nested_3-14             463558      2506 ns/op     1392 B/op      39 allocs/op
BenchmarkGoFlow_Subflow_Nested_3-14             458702      2521 ns/op     1392 B/op      39 allocs/op
BenchmarkGoFlow_Subflow_Nested_3-14             457734      2522 ns/op     1392 B/op      39 allocs/op
BenchmarkGoFlow_Subflow_Wide-14                  14710     80856 ns/op    19231 B/op     580 allocs/op
BenchmarkGoFlow_Subflow_Wide-14                  14688     82490 ns/op    19234 B/op     580 allocs/op
BenchmarkGoFlow_Subflow_Wide-14                  14937     80817 ns/op    19226 B/op     580 allocs/op
BenchmarkGoFlow_CondLoop_10-14                  169734      7084 ns/op     1144 B/op      77 allocs/op
BenchmarkGoFlow_CondLoop_10-14                  171328      7162 ns/op     1144 B/op      77 allocs/op
BenchmarkGoFlow_CondLoop_10-14                  175246      7067 ns/op     1144 B/op      77 allocs/op
BenchmarkGoFlow_CondLoop_100-14                  17886     70043 ns/op    10509 B/op     707 allocs/op
BenchmarkGoFlow_CondLoop_100-14                  16692     67463 ns/op    10507 B/op     707 allocs/op
BenchmarkGoFlow_CondLoop_100-14                  17264     67963 ns/op    10508 B/op     707 allocs/op
BenchmarkGoFlow_CPUBound_C32-14                  61567     19606 ns/op     1699 B/op      80 allocs/op
BenchmarkGoFlow_CPUBound_C32-14                  55620     20532 ns/op     1701 B/op      80 allocs/op
BenchmarkGoFlow_CPUBound_C32-14                  54285     20712 ns/op     1702 B/op      80 allocs/op
BenchmarkGoFlow_CPUBound_Pipeline4x8-14          62661     19296 ns/op     3243 B/op     109 allocs/op
BenchmarkGoFlow_CPUBound_Pipeline4x8-14          62637     19134 ns/op     3243 B/op     109 allocs/op
BenchmarkGoFlow_CPUBound_Pipeline4x8-14          63198     19597 ns/op     3243 B/op     109 allocs/op
BenchmarkGoFlow_RepeatedRun_10-14                16891     72001 ns/op     8251 B/op     436 allocs/op
BenchmarkGoFlow_RepeatedRun_10-14                17098     69957 ns/op     8244 B/op     437 allocs/op
BenchmarkGoFlow_RepeatedRun_10-14                17367     70903 ns/op     8249 B/op     436 allocs/op
BenchmarkGoTaskflow_C32-14                       44214     27480 ns/op     7757 B/op     227 allocs/op
BenchmarkGoTaskflow_C32-14                       42482     27676 ns/op     7756 B/op     227 allocs/op
BenchmarkGoTaskflow_C32-14                       43705     27803 ns/op     7757 B/op     227 allocs/op
BenchmarkGoTaskflow_S32-14                       25466     47327 ns/op     7923 B/op     286 allocs/op
BenchmarkGoTaskflow_S32-14                       25022     49662 ns/op     7923 B/op     286 allocs/op
BenchmarkGoTaskflow_S32-14                       24144     49024 ns/op     7923 B/op     286 allocs/op
BenchmarkGoTaskflow_C6-14                       158961      7581 ns/op     1491 B/op      52 allocs/op
BenchmarkGoTaskflow_C6-14                       152186      7854 ns/op     1491 B/op      52 allocs/op
BenchmarkGoTaskflow_C6-14                       156475      7634 ns/op     1491 B/op      52 allocs/op
BenchmarkGoTaskflow_C8x8-14                      18736     66276 ns/op    25180 B/op     560 allocs/op
BenchmarkGoTaskflow_C8x8-14                      18429     65589 ns/op    25180 B/op     560 allocs/op
BenchmarkGoTaskflow_C8x8-14                      18544     68303 ns/op    25181 B/op     560 allocs/op
BenchmarkGoTaskflow_C256-14                       4089    282474 ns/op    66149 B/op    1811 allocs/op
BenchmarkGoTaskflow_C256-14                       4111    270622 ns/op    65988 B/op    1810 allocs/op
BenchmarkGoTaskflow_C256-14                       4479    276276 ns/op    66054 B/op    1811 allocs/op
BenchmarkGoTaskflow_C1024-14                      1110   1112522 ns/op   267584 B/op    7227 allocs/op
BenchmarkGoTaskflow_C1024-14                      1100   1107237 ns/op   267684 B/op    7227 allocs/op
BenchmarkGoTaskflow_C1024-14                      1113   1101933 ns/op   267567 B/op    7227 allocs/op
BenchmarkGoTaskflow_S128-14                       6242    191433 ns/op    31763 B/op    1150 allocs/op
BenchmarkGoTaskflow_S128-14                       6338    189521 ns/op    31763 B/op    1150 allocs/op
BenchmarkGoTaskflow_S128-14                       6519    187860 ns/op    31763 B/op    1150 allocs/op
BenchmarkGoTaskflow_FanOutFanIn_64-14            17080     68942 ns/op    19797 B/op     598 allocs/op
BenchmarkGoTaskflow_FanOutFanIn_64-14            17365     70237 ns/op    19809 B/op     599 allocs/op
BenchmarkGoTaskflow_FanOutFanIn_64-14            16218     74322 ns/op    19810 B/op     599 allocs/op
BenchmarkGoTaskflow_FanOutFanIn_256-14            3578    301954 ns/op    80048 B/op    2340 allocs/op
BenchmarkGoTaskflow_FanOutFanIn_256-14            3757    300079 ns/op    79954 B/op    2340 allocs/op
BenchmarkGoTaskflow_FanOutFanIn_256-14            4011    287922 ns/op    80062 B/op    2340 allocs/op
BenchmarkGoTaskflow_Pipeline_4x16-14             15158     77632 ns/op    32908 B/op     545 allocs/op
BenchmarkGoTaskflow_Pipeline_4x16-14             16484     73087 ns/op    32906 B/op     545 allocs/op
BenchmarkGoTaskflow_Pipeline_4x16-14             16659     73176 ns/op    32906 B/op     545 allocs/op
BenchmarkGoTaskflow_Pipeline_8x32-14              2366    477705 ns/op   235529 B/op    2274 allocs/op
BenchmarkGoTaskflow_Pipeline_8x32-14              2354    492843 ns/op   235494 B/op    2274 allocs/op
BenchmarkGoTaskflow_Pipeline_8x32-14              2458    483679 ns/op   235537 B/op    2274 allocs/op
BenchmarkGoTaskflow_Pipeline_16x8-14              9398    128904 ns/op    51877 B/op    1137 allocs/op
BenchmarkGoTaskflow_Pipeline_16x8-14              9002    130776 ns/op    51875 B/op    1137 allocs/op
BenchmarkGoTaskflow_Pipeline_16x8-14              9288    130638 ns/op    51876 B/op    1137 allocs/op
BenchmarkGoTaskflow_BinaryTree_depth6-14          6986    169903 ns/op    33623 B/op    1087 allocs/op
BenchmarkGoTaskflow_BinaryTree_depth6-14          7510    168253 ns/op    33625 B/op    1087 allocs/op
BenchmarkGoTaskflow_BinaryTree_depth6-14          7386    167438 ns/op    33623 B/op    1087 allocs/op
BenchmarkGoTaskflow_DiamondChain_16-14           19707     61321 ns/op    12536 B/op     439 allocs/op
BenchmarkGoTaskflow_DiamondChain_16-14           18622     63511 ns/op    12535 B/op     439 allocs/op
BenchmarkGoTaskflow_DiamondChain_16-14           19405     62843 ns/op    12536 B/op     439 allocs/op
BenchmarkGoTaskflow_CPUBound_C32-14              38166     31371 ns/op     7789 B/op     227 allocs/op
BenchmarkGoTaskflow_CPUBound_C32-14              37114     31077 ns/op     7787 B/op     227 allocs/op
BenchmarkGoTaskflow_CPUBound_C32-14              38398     31168 ns/op     7790 B/op     227 allocs/op
BenchmarkGoTaskflow_CPUBound_Pipeline4x8-14      32628     37671 ns/op    11807 B/op     272 allocs/op
BenchmarkGoTaskflow_CPUBound_Pipeline4x8-14      32262     39677 ns/op    11807 B/op     272 allocs/op
BenchmarkGoTaskflow_CPUBound_Pipeline4x8-14      31629     36408 ns/op    11806 B/op     272 allocs/op
BenchmarkGoTaskflow_Subflow_Static-14           177075      6563 ns/op     1218 B/op      41 allocs/op
BenchmarkGoTaskflow_Subflow_Static-14           175680      6621 ns/op     1218 B/op      41 allocs/op
BenchmarkGoTaskflow_Subflow_Static-14           182406      6610 ns/op     1218 B/op      41 allocs/op
BenchmarkGoTaskflow_RepeatedRun_10-14             9555    120617 ns/op    36054 B/op    1121 allocs/op
BenchmarkGoTaskflow_RepeatedRun_10-14            10000    127507 ns/op    36059 B/op    1121 allocs/op
BenchmarkGoTaskflow_RepeatedRun_10-14             8773    124082 ns/op    36053 B/op    1121 allocs/op
```
