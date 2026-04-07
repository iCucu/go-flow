package goflow

import (
	"bytes"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func newTestExecutor() Executor {
	return NewExecutor(uint(runtime.NumCPU()))
}

// --- Basic DAG tests ---

func TestLinearChain(t *testing.T) {
	var order []string
	var mu sync.Mutex
	record := func(s string) {
		mu.Lock()
		order = append(order, s)
		mu.Unlock()
	}

	tf := NewTaskFlow("linear")
	a := tf.NewTask("A", func() { record("A") })
	b := tf.NewTask("B", func() { record("B") })
	c := tf.NewTask("C", func() { record("C") })
	a.Precede(b)
	b.Precede(c)

	newTestExecutor().Run(tf).Wait()

	if len(order) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(order))
	}
	idx := func(s string) int {
		for i, v := range order {
			if v == s {
				return i
			}
		}
		return -1
	}
	if idx("A") >= idx("B") || idx("B") >= idx("C") {
		t.Fatalf("order violated: %v", order)
	}
}

func TestDiamondDAG(t *testing.T) {
	var order []string
	var mu sync.Mutex
	record := func(s string) {
		mu.Lock()
		order = append(order, s)
		mu.Unlock()
	}

	tf := NewTaskFlow("diamond")
	a := tf.NewTask("A", func() { record("A") })
	b := tf.NewTask("B", func() { record("B") })
	c := tf.NewTask("C", func() { record("C") })
	d := tf.NewTask("D", func() { record("D") })

	a.Precede(b, c)
	d.Succeed(b, c)

	newTestExecutor().Run(tf).Wait()

	if len(order) != 4 {
		t.Fatalf("expected 4 tasks, got %d", len(order))
	}
	idx := func(s string) int {
		for i, v := range order {
			if v == s {
				return i
			}
		}
		return -1
	}
	if idx("A") >= idx("B") || idx("A") >= idx("C") {
		t.Fatalf("A should run before B and C: %v", order)
	}
	if idx("B") >= idx("D") || idx("C") >= idx("D") {
		t.Fatalf("D should run after B and C: %v", order)
	}
}

func TestFullyParallel32(t *testing.T) {
	var count atomic.Int32
	tf := NewTaskFlow("parallel32")
	for i := 0; i < 32; i++ {
		tf.NewTask("N", func() { count.Add(1) })
	}

	newTestExecutor().Run(tf).Wait()

	if count.Load() != 32 {
		t.Fatalf("expected 32, got %d", count.Load())
	}
}

// --- Subflow tests ---

func TestStaticSubflow(t *testing.T) {
	var order []string
	var mu sync.Mutex
	record := func(s string) {
		mu.Lock()
		order = append(order, s)
		mu.Unlock()
	}

	tf := NewTaskFlow("sf-test")
	a := tf.NewTask("A", func() { record("A") })
	sf := tf.NewSubflow("SF", func(sf *Subflow) {
		s1 := sf.NewTask("S1", func() { record("S1") })
		s2 := sf.NewTask("S2", func() { record("S2") })
		s1.Precede(s2)
	})
	b := tf.NewTask("B", func() { record("B") })

	a.Precede(sf)
	sf.Precede(b)

	newTestExecutor().Run(tf).Wait()

	if len(order) != 4 {
		t.Fatalf("expected 4 tasks, got %d: %v", len(order), order)
	}

	idx := func(s string) int {
		for i, v := range order {
			if v == s {
				return i
			}
		}
		return -1
	}
	if idx("A") >= idx("S1") {
		t.Fatalf("A should run before S1: %v", order)
	}
	if idx("S1") >= idx("S2") {
		t.Fatalf("S1 should run before S2: %v", order)
	}
	if idx("S2") >= idx("B") {
		t.Fatalf("S2 should run before B: %v", order)
	}
}

func TestDynamicSubflow(t *testing.T) {
	var callCount atomic.Int32
	var lastTaskCount atomic.Int32

	tf := NewTaskFlow("dynamic")
	tf.NewSubflow("DSF", func(sf *Subflow) {
		n := int(callCount.Add(1))
		lastTaskCount.Store(0)
		for i := 0; i < n; i++ {
			sf.NewTask("dyn", func() { lastTaskCount.Add(1) })
		}
	})

	exec := newTestExecutor()

	// First run: 1 call -> 1 task
	exec.Run(tf).Wait()
	if callCount.Load() != 1 {
		t.Fatalf("expected callCount=1, got %d", callCount.Load())
	}
	if lastTaskCount.Load() != 1 {
		t.Fatalf("expected 1 task in first run, got %d", lastTaskCount.Load())
	}

	// Second run: 2 calls total -> 2 tasks
	tf.Reset()
	exec.Run(tf).Wait()
	if callCount.Load() != 2 {
		t.Fatalf("expected callCount=2, got %d", callCount.Load())
	}
	if lastTaskCount.Load() != 2 {
		t.Fatalf("expected 2 tasks in second run, got %d", lastTaskCount.Load())
	}

	// Third run: 3 calls total -> 3 tasks
	tf.Reset()
	exec.Run(tf).Wait()
	if callCount.Load() != 3 {
		t.Fatalf("expected callCount=3, got %d", callCount.Load())
	}
	if lastTaskCount.Load() != 3 {
		t.Fatalf("expected 3 tasks in third run, got %d", lastTaskCount.Load())
	}
}

func TestNestedSubflow(t *testing.T) {
	var deepest atomic.Bool

	tf := NewTaskFlow("nested")
	tf.NewSubflow("outer", func(sf *Subflow) {
		sf.NewSubflow("inner", func(sf2 *Subflow) {
			sf2.NewTask("leaf", func() {
				deepest.Store(true)
			})
		})
	})

	newTestExecutor().Run(tf).Wait()

	if !deepest.Load() {
		t.Fatal("nested subflow leaf task did not execute")
	}
}

// --- Condition tests ---

func TestConditionBasicBranch(t *testing.T) {
	var branchA, branchB atomic.Bool

	tf := NewTaskFlow("cond")
	a := tf.NewTask("A", func() { branchA.Store(true) })
	b := tf.NewTask("B", func() { branchB.Store(true) })
	cond := tf.NewCondition("cond", func() uint { return 1 })

	cond.Precede(a, b) // 0->A, 1->B

	newTestExecutor().Run(tf).Wait()

	if branchA.Load() {
		t.Fatal("branch A should not have executed")
	}
	if !branchB.Load() {
		t.Fatal("branch B should have executed")
	}
}

func TestConditionLoop(t *testing.T) {
	var counter atomic.Int32

	tf := NewTaskFlow("loop")
	init := tf.NewTask("init", func() { counter.Store(0) })
	body := tf.NewTask("body", func() { counter.Add(1) })
	done := tf.NewTask("done", func() {})

	cond := tf.NewCondition("cond", func() uint {
		if counter.Load() < 5 {
			return 0 // loop back
		}
		return 1 // exit
	})

	init.Precede(body)
	body.Precede(cond)
	cond.Precede(body, done) // 0->body, 1->done

	newTestExecutor().Run(tf).Wait()

	if counter.Load() != 5 {
		t.Fatalf("expected counter=5, got %d", counter.Load())
	}
}

// --- Priority test ---

func TestPriorityScheduling(t *testing.T) {
	var order []string
	var mu sync.Mutex
	record := func(s string) {
		mu.Lock()
		order = append(order, s)
		mu.Unlock()
	}

	// Use concurrency=1 to make scheduling order deterministic.
	exec := NewExecutor(1)

	tf := NewTaskFlow("prio")
	start := tf.NewTask("start", func() { record("start") })

	high := tf.NewTask("high", func() { record("high") })
	high.Priority(HIGH)

	low := tf.NewTask("low", func() { record("low") })
	low.Priority(LOW)

	normal := tf.NewTask("normal", func() { record("normal") })
	normal.Priority(NORMAL)

	start.Precede(high, low, normal)

	exec.Run(tf).Wait()

	if len(order) != 4 {
		t.Fatalf("expected 4 tasks, got %d: %v", len(order), order)
	}

	// After start, high should come before normal, normal before low.
	idx := func(s string) int {
		for i, v := range order {
			if v == s {
				return i
			}
		}
		return -1
	}
	if idx("high") >= idx("normal") || idx("normal") >= idx("low") {
		t.Fatalf("priority order violated: %v", order)
	}
}

// --- Edge cases ---

func TestEmptyTaskFlow(t *testing.T) {
	tf := NewTaskFlow("empty")
	newTestExecutor().Run(tf).Wait()
}

func TestSingleNode(t *testing.T) {
	var ran atomic.Bool
	tf := NewTaskFlow("single")
	tf.NewTask("only", func() { ran.Store(true) })

	newTestExecutor().Run(tf).Wait()

	if !ran.Load() {
		t.Fatal("single task did not execute")
	}
}

func TestPanicCancelsGraph(t *testing.T) {
	var afterPanic atomic.Bool

	tf := NewTaskFlow("panic")
	p := tf.NewTask("panicker", func() { panic("boom") })
	after := tf.NewTask("after", func() { afterPanic.Store(true) })
	p.Precede(after)

	newTestExecutor().Run(tf).Wait()

	if afterPanic.Load() {
		t.Fatal("task after panic should not have executed")
	}
}

func TestDumpDOT(t *testing.T) {
	tf := NewTaskFlow("viz")
	a := tf.NewTask("A", func() {})
	b := tf.NewTask("B", func() {})
	a.Precede(b)

	var buf bytes.Buffer
	if err := tf.Dump(&buf); err != nil {
		t.Fatal(err)
	}
	dot := buf.String()
	if !strings.Contains(dot, `"A" -> "B"`) {
		t.Fatalf("DOT output missing edge: %s", dot)
	}
}

func TestTaskName(t *testing.T) {
	tf := NewTaskFlow("names")
	a := tf.NewTask("myTask", func() {})
	if a.Name() != "myTask" {
		t.Fatalf("expected 'myTask', got %q", a.Name())
	}
	if tf.Name() != "names" {
		t.Fatalf("expected 'names', got %q", tf.Name())
	}
}

// --- Concurrency stress test ---

func TestConcurrencyStress(t *testing.T) {
	var sum atomic.Int64
	tf := NewTaskFlow("stress")
	for i := 0; i < 1000; i++ {
		tf.NewTask("N", func() { sum.Add(1) })
	}

	newTestExecutor().Run(tf).Wait()

	if sum.Load() != 1000 {
		t.Fatalf("expected 1000, got %d", sum.Load())
	}
}

func TestMultipleRunsSameTaskflow(t *testing.T) {
	var count atomic.Int32
	tf := NewTaskFlow("rerun")
	tf.NewTask("inc", func() { count.Add(1) })

	exec := newTestExecutor()
	exec.Run(tf).Wait()
	tf.Reset()
	exec.Run(tf).Wait()
	tf.Reset()
	exec.Run(tf).Wait()

	if count.Load() != 3 {
		t.Fatalf("expected 3 runs, got %d", count.Load())
	}
}

func TestConditionWithSubflow(t *testing.T) {
	var sfRan atomic.Bool

	tf := NewTaskFlow("cond-sf")
	sf := tf.NewSubflow("sf", func(sf *Subflow) {
		sf.NewTask("inner", func() { sfRan.Store(true) })
	})
	noop := tf.NewTask("noop", func() {})

	cond := tf.NewCondition("pick", func() uint { return 0 })
	cond.Precede(sf, noop)

	newTestExecutor().Run(tf).Wait()

	if !sfRan.Load() {
		t.Fatal("subflow should have run via condition branch 0")
	}
}

func TestFrozenTaskflowPanics(t *testing.T) {
	tf := NewTaskFlow("freeze")
	tf.NewTask("A", func() {})

	exec := newTestExecutor()
	exec.Run(tf).Wait()

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic when adding task to frozen taskflow")
		}
	}()
	tf.NewTask("B", func() {})
}

func TestWideDAG(t *testing.T) {
	var count atomic.Int32
	tf := NewTaskFlow("wide")
	start := tf.NewTask("start", func() {})
	end := tf.NewTask("end", func() {})

	for i := 0; i < 100; i++ {
		mid := tf.NewTask("mid", func() { count.Add(1) })
		start.Precede(mid)
		mid.Precede(end)
	}

	newTestExecutor().Run(tf).Wait()

	if count.Load() != 100 {
		t.Fatalf("expected 100, got %d", count.Load())
	}
}

// --- Complex DAG topology tests ---

// orderTracker records execution timestamps so we can verify all DAG edges.
type orderTracker struct {
	mu  sync.Mutex
	seq map[string]int
	cnt int
}

func newOrderTracker() *orderTracker {
	return &orderTracker{seq: make(map[string]int)}
}

func (o *orderTracker) record(name string) {
	o.mu.Lock()
	o.cnt++
	o.seq[name] = o.cnt
	o.mu.Unlock()
}

// assertBefore checks that a ran before b.
func (o *orderTracker) assertBefore(t *testing.T, a, b string) {
	t.Helper()
	sa, oka := o.seq[a]
	sb, okb := o.seq[b]
	if !oka {
		t.Fatalf("task %q was not executed", a)
	}
	if !okb {
		t.Fatalf("task %q was not executed", b)
	}
	if sa >= sb {
		t.Fatalf("expected %q (seq %d) before %q (seq %d)", a, sa, b, sb)
	}
}

func (o *orderTracker) assertCount(t *testing.T, expected int) {
	t.Helper()
	if o.cnt != expected {
		t.Fatalf("expected %d tasks executed, got %d", expected, o.cnt)
	}
}

// TestMultiSourceMultiSink: multiple roots and multiple leaves.
//
//	A   B
//	|\ /|
//	| X |
//	|/ \|
//	C   D
//	|   |
//	E   F
func TestMultiSourceMultiSink(t *testing.T) {
	o := newOrderTracker()
	tf := NewTaskFlow("msms")

	a := tf.NewTask("A", func() { o.record("A") })
	b := tf.NewTask("B", func() { o.record("B") })
	c := tf.NewTask("C", func() { o.record("C") })
	d := tf.NewTask("D", func() { o.record("D") })
	e := tf.NewTask("E", func() { o.record("E") })
	f := tf.NewTask("F", func() { o.record("F") })

	a.Precede(c, d)
	b.Precede(c, d)
	c.Precede(e)
	d.Precede(f)

	newTestExecutor().Run(tf).Wait()

	o.assertCount(t, 6)
	o.assertBefore(t, "A", "C")
	o.assertBefore(t, "B", "C")
	o.assertBefore(t, "A", "D")
	o.assertBefore(t, "B", "D")
	o.assertBefore(t, "C", "E")
	o.assertBefore(t, "D", "F")
}

// TestWShapedDAG: split-merge-split-merge pattern.
//
//	    A
//	   / \
//	  B   C
//	   \ /
//	    D
//	   / \
//	  E   F
//	   \ /
//	    G
func TestWShapedDAG(t *testing.T) {
	o := newOrderTracker()
	tf := NewTaskFlow("W")

	a := tf.NewTask("A", func() { o.record("A") })
	b := tf.NewTask("B", func() { o.record("B") })
	c := tf.NewTask("C", func() { o.record("C") })
	d := tf.NewTask("D", func() { o.record("D") })
	e := tf.NewTask("E", func() { o.record("E") })
	f := tf.NewTask("F", func() { o.record("F") })
	g := tf.NewTask("G", func() { o.record("G") })

	a.Precede(b, c)
	d.Succeed(b, c)
	d.Precede(e, f)
	g.Succeed(e, f)

	newTestExecutor().Run(tf).Wait()

	o.assertCount(t, 7)
	o.assertBefore(t, "A", "B")
	o.assertBefore(t, "A", "C")
	o.assertBefore(t, "B", "D")
	o.assertBefore(t, "C", "D")
	o.assertBefore(t, "D", "E")
	o.assertBefore(t, "D", "F")
	o.assertBefore(t, "E", "G")
	o.assertBefore(t, "F", "G")
}

// TestHourglassDAG: fan-in then fan-out.
//
//	A  B  C
//	 \ | /
//	   D
//	 / | \
//	E  F  G
func TestHourglassDAG(t *testing.T) {
	o := newOrderTracker()
	tf := NewTaskFlow("hourglass")

	a := tf.NewTask("A", func() { o.record("A") })
	b := tf.NewTask("B", func() { o.record("B") })
	c := tf.NewTask("C", func() { o.record("C") })
	d := tf.NewTask("D", func() { o.record("D") })
	e := tf.NewTask("E", func() { o.record("E") })
	f := tf.NewTask("F", func() { o.record("F") })
	g := tf.NewTask("G", func() { o.record("G") })

	d.Succeed(a, b, c)
	d.Precede(e, f, g)

	newTestExecutor().Run(tf).Wait()

	o.assertCount(t, 7)
	for _, src := range []string{"A", "B", "C"} {
		o.assertBefore(t, src, "D")
	}
	for _, dst := range []string{"E", "F", "G"} {
		o.assertBefore(t, "D", dst)
	}
}

// TestLongDiamondChain: 20 sequential diamonds, verifying full ordering.
//
//	start → [L0,R0] → J0 → [L1,R1] → J1 → ... → J19 → end
func TestLongDiamondChain(t *testing.T) {
	o := newOrderTracker()
	tf := NewTaskFlow("longdiamond")

	start := tf.NewTask("start", func() { o.record("start") })
	end := tf.NewTask("end", func() { o.record("end") })

	prev := start
	for i := 0; i < 20; i++ {
		ln := fmt.Sprintf("L%d", i)
		rn := fmt.Sprintf("R%d", i)
		jn := fmt.Sprintf("J%d", i)
		left := tf.NewTask(ln, func() { o.record(ln) })
		right := tf.NewTask(rn, func() { o.record(rn) })
		join := tf.NewTask(jn, func() { o.record(jn) })
		prev.Precede(left, right)
		join.Succeed(left, right)
		prev = join
	}
	prev.Precede(end)

	newTestExecutor().Run(tf).Wait()

	o.assertCount(t, 2+20*3) // start + end + 20*(left+right+join)
	o.assertBefore(t, "start", "L0")
	o.assertBefore(t, "start", "R0")
	for i := 0; i < 20; i++ {
		ln := fmt.Sprintf("L%d", i)
		rn := fmt.Sprintf("R%d", i)
		jn := fmt.Sprintf("J%d", i)
		o.assertBefore(t, ln, jn)
		o.assertBefore(t, rn, jn)
		if i < 19 {
			nextL := fmt.Sprintf("L%d", i+1)
			nextR := fmt.Sprintf("R%d", i+1)
			o.assertBefore(t, jn, nextL)
			o.assertBefore(t, jn, nextR)
		}
	}
	o.assertBefore(t, "J19", "end")
}

// TestLayeredPipelineWithCrossDeps: 4 layers, each layer feeds into all nodes of the next layer.
// Total: 4 layers x 4 nodes = 16 nodes. Every node in layer N depends on all nodes in layer N-1.
func TestLayeredPipelineWithCrossDeps(t *testing.T) {
	o := newOrderTracker()
	tf := NewTaskFlow("layered")

	layers := 4
	width := 4
	names := make([][]string, layers)
	var prevLayer []*Task

	for l := 0; l < layers; l++ {
		var curLayer []*Task
		for w := 0; w < width; w++ {
			name := fmt.Sprintf("L%dN%d", l, w)
			names[l] = append(names[l], name)
			task := tf.NewTask(name, func() { o.record(name) })
			for _, p := range prevLayer {
				p.Precede(task)
			}
			curLayer = append(curLayer, task)
		}
		prevLayer = curLayer
	}

	newTestExecutor().Run(tf).Wait()

	o.assertCount(t, layers*width)
	for l := 1; l < layers; l++ {
		for _, cur := range names[l] {
			for _, prev := range names[l-1] {
				o.assertBefore(t, prev, cur)
			}
		}
	}
}

// TestIrregularDAG: asymmetric structure with complex dependency patterns.
//
//	A ──→ B ──→ D ──→ G
//	│     │     ↑     ↑
//	│     └──→ E ─────┘
//	│           ↑
//	└──→ C ─────┘
//	     │
//	     └──→ F
func TestIrregularDAG(t *testing.T) {
	o := newOrderTracker()
	tf := NewTaskFlow("irregular")

	a := tf.NewTask("A", func() { o.record("A") })
	b := tf.NewTask("B", func() { o.record("B") })
	c := tf.NewTask("C", func() { o.record("C") })
	d := tf.NewTask("D", func() { o.record("D") })
	e := tf.NewTask("E", func() { o.record("E") })
	f := tf.NewTask("F", func() { o.record("F") })
	g := tf.NewTask("G", func() { o.record("G") })

	a.Precede(b, c)
	b.Precede(d, e)
	c.Precede(e, f)
	e.Precede(d, g)
	d.Precede(g)

	newTestExecutor().Run(tf).Wait()

	o.assertCount(t, 7)
	o.assertBefore(t, "A", "B")
	o.assertBefore(t, "A", "C")
	o.assertBefore(t, "B", "D")
	o.assertBefore(t, "B", "E")
	o.assertBefore(t, "C", "E")
	o.assertBefore(t, "C", "F")
	o.assertBefore(t, "E", "D")
	o.assertBefore(t, "E", "G")
	o.assertBefore(t, "D", "G")
}

// TestParallelChainsConverge: N independent chains that all converge into a single sink.
//
//	chain 0: C0_0 → C0_1 → C0_2 ──┐
//	chain 1: C1_0 → C1_1 → C1_2 ──┤→ sink
//	...                             │
//	chain 7: C7_0 → C7_1 → C7_2 ──┘
func TestParallelChainsConverge(t *testing.T) {
	o := newOrderTracker()
	tf := NewTaskFlow("converge")

	numChains := 8
	chainLen := 3

	sink := tf.NewTask("sink", func() { o.record("sink") })
	names := make([][]string, numChains)

	for c := 0; c < numChains; c++ {
		var prev *Task
		for s := 0; s < chainLen; s++ {
			name := fmt.Sprintf("C%d_%d", c, s)
			names[c] = append(names[c], name)
			task := tf.NewTask(name, func() { o.record(name) })
			if prev != nil {
				prev.Precede(task)
			}
			prev = task
		}
		prev.Precede(sink)
	}

	newTestExecutor().Run(tf).Wait()

	o.assertCount(t, numChains*chainLen+1)
	for c := 0; c < numChains; c++ {
		for s := 1; s < chainLen; s++ {
			o.assertBefore(t, names[c][s-1], names[c][s])
		}
		o.assertBefore(t, names[c][chainLen-1], "sink")
	}
}

// TestComplexMultiPathDAG: node reachable by multiple paths of different lengths.
//
//	A → B → C → F
//	A → D → F
//	A → E → F
//	F must run after A,B,C,D,E
func TestComplexMultiPathDAG(t *testing.T) {
	o := newOrderTracker()
	tf := NewTaskFlow("multipath")

	a := tf.NewTask("A", func() { o.record("A") })
	b := tf.NewTask("B", func() { o.record("B") })
	c := tf.NewTask("C", func() { o.record("C") })
	d := tf.NewTask("D", func() { o.record("D") })
	e := tf.NewTask("E", func() { o.record("E") })
	f := tf.NewTask("F", func() { o.record("F") })

	a.Precede(b, d, e)
	b.Precede(c)
	c.Precede(f)
	d.Precede(f)
	e.Precede(f)

	newTestExecutor().Run(tf).Wait()

	o.assertCount(t, 6)
	o.assertBefore(t, "A", "B")
	o.assertBefore(t, "A", "D")
	o.assertBefore(t, "A", "E")
	o.assertBefore(t, "B", "C")
	o.assertBefore(t, "C", "F")
	o.assertBefore(t, "D", "F")
	o.assertBefore(t, "E", "F")
}

// --- Operator tests ---

type adder struct {
	val    int
	target *atomic.Int64
}

func (a *adder) Compute() {
	a.target.Add(int64(a.val))
}

func TestOperatorBasic(t *testing.T) {
	var sum atomic.Int64
	tf := NewTaskFlow("op-basic")

	tf.NewOperatorTask("add10", &adder{val: 10, target: &sum})
	tf.NewOperatorTask("add20", &adder{val: 20, target: &sum})
	tf.NewOperatorTask("add30", &adder{val: 30, target: &sum})

	newTestExecutor().Run(tf).Wait()

	if sum.Load() != 60 {
		t.Fatalf("expected sum=60, got %d", sum.Load())
	}
}

func TestOperatorDAGOrder(t *testing.T) {
	o := newOrderTracker()

	type recorder struct {
		name    string
		tracker *orderTracker
	}
	newRec := func(name string) *recorder { return &recorder{name: name, tracker: o} }

	// Implement Compute on recorder
	tf := NewTaskFlow("op-dag")
	a := tf.NewOperatorTask("A", &recOp{name: "A", o: o})
	b := tf.NewOperatorTask("B", &recOp{name: "B", o: o})
	c := tf.NewOperatorTask("C", &recOp{name: "C", o: o})
	d := tf.NewOperatorTask("D", &recOp{name: "D", o: o})

	_ = newRec // suppress unused

	a.Precede(b, c)
	d.Succeed(b, c)

	newTestExecutor().Run(tf).Wait()

	o.assertCount(t, 4)
	o.assertBefore(t, "A", "B")
	o.assertBefore(t, "A", "C")
	o.assertBefore(t, "B", "D")
	o.assertBefore(t, "C", "D")
}

type recOp struct {
	name string
	o    *orderTracker
}

func (r *recOp) Compute() { r.o.record(r.name) }

func TestOperatorWithFuncMixed(t *testing.T) {
	o := newOrderTracker()
	tf := NewTaskFlow("mixed")

	a := tf.NewTask("A", func() { o.record("A") })
	b := tf.NewOperatorTask("B", &recOp{name: "B", o: o})
	c := tf.NewTask("C", func() { o.record("C") })

	a.Precede(b)
	b.Precede(c)

	newTestExecutor().Run(tf).Wait()

	o.assertCount(t, 3)
	o.assertBefore(t, "A", "B")
	o.assertBefore(t, "B", "C")
}

func TestFuncOperatorAdapter(t *testing.T) {
	var ran atomic.Bool
	op := FuncOperator(func() { ran.Store(true) })

	tf := NewTaskFlow("func-op")
	tf.NewOperatorTask("adapted", op)

	newTestExecutor().Run(tf).Wait()

	if !ran.Load() {
		t.Fatal("FuncOperator task did not execute")
	}
}

func TestOperatorInSubflow(t *testing.T) {
	var sum atomic.Int64
	tf := NewTaskFlow("op-sf")

	tf.NewSubflow("sf", func(sf *Subflow) {
		sf.NewOperatorTask("add5", &adder{val: 5, target: &sum})
		sf.NewOperatorTask("add15", &adder{val: 15, target: &sum})
	})

	newTestExecutor().Run(tf).Wait()

	if sum.Load() != 20 {
		t.Fatalf("expected sum=20, got %d", sum.Load())
	}
}

// statefulCounter is an Operator that increments itself on each Compute call,
// demonstrating that Operator instances carry mutable state across invocations.
type statefulCounter struct {
	count atomic.Int32
}

func (s *statefulCounter) Compute() { s.count.Add(1) }

func TestOperatorStateful(t *testing.T) {
	counter := &statefulCounter{}

	tf := NewTaskFlow("stateful")
	tf.NewOperatorTask("inc", counter)

	exec := newTestExecutor()
	exec.Run(tf).Wait()
	tf.Reset()
	exec.Run(tf).Wait()
	tf.Reset()
	exec.Run(tf).Wait()

	if counter.count.Load() != 3 {
		t.Fatalf("expected count=3, got %d", counter.count.Load())
	}
}

func TestOperatorPriority(t *testing.T) {
	o := newOrderTracker()
	exec := NewExecutor(1)

	tf := NewTaskFlow("op-prio")
	start := tf.NewTask("start", func() { o.record("start") })

	high := tf.NewOperatorTask("high", &recOp{name: "high", o: o})
	high.Priority(HIGH)

	low := tf.NewOperatorTask("low", &recOp{name: "low", o: o})
	low.Priority(LOW)

	start.Precede(high, low)

	exec.Run(tf).Wait()

	o.assertCount(t, 3)
	o.assertBefore(t, "start", "high")
	o.assertBefore(t, "high", "low")
}
