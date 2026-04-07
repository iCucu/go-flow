package goflow

import (
	"fmt"
	"math"
	"sync/atomic"
	"testing"

	gtf "github.com/noneback/go-taskflow"
)

// ---------- go-flow benchmarks ----------

var goflowExec = NewExecutor(6400)

// --- Original scenarios (matching go-taskflow's benchmarks) ---

func BenchmarkGoFlow_C32(b *testing.B) {
	tf := NewTaskFlow("G")
	for i := 0; i < 32; i++ {
		tf.NewTask(fmt.Sprintf("N%d", i), func() {})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tf.Reset()
		goflowExec.Run(tf).Wait()
	}
}

func BenchmarkGoFlow_S32(b *testing.B) {
	tf := NewTaskFlow("G")
	prev := tf.NewTask("N0", func() {})
	for i := 1; i < 32; i++ {
		next := tf.NewTask(fmt.Sprintf("N%d", i), func() {})
		prev.Precede(next)
		prev = next
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tf.Reset()
		goflowExec.Run(tf).Wait()
	}
}

func BenchmarkGoFlow_C6(b *testing.B) {
	tf := NewTaskFlow("G")
	n0 := tf.NewTask("N0", func() {})
	n1 := tf.NewTask("N1", func() {})
	n2 := tf.NewTask("N2", func() {})
	n3 := tf.NewTask("N3", func() {})
	n4 := tf.NewTask("N4", func() {})
	n5 := tf.NewTask("N5", func() {})
	n0.Precede(n1, n2)
	n1.Precede(n3)
	n2.Precede(n4)
	n5.Succeed(n3, n4)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tf.Reset()
		goflowExec.Run(tf).Wait()
	}
}

func BenchmarkGoFlow_C8x8(b *testing.B) {
	tf := NewTaskFlow("G")
	layersCount := 8
	layerNodesCount := 8
	var curLayer, upperLayer []*Task
	for i := 0; i < layersCount; i++ {
		for j := 0; j < layerNodesCount; j++ {
			task := tf.NewTask(fmt.Sprintf("N%d", i*layersCount+j), func() {})
			for k := range upperLayer {
				upperLayer[k].Precede(task)
			}
			curLayer = append(curLayer, task)
		}
		upperLayer = curLayer
		curLayer = []*Task{}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tf.Reset()
		goflowExec.Run(tf).Wait()
	}
}

// --- Large-scale concurrent ---

func BenchmarkGoFlow_C256(b *testing.B) {
	tf := NewTaskFlow("G")
	for i := 0; i < 256; i++ {
		tf.NewTask(fmt.Sprintf("N%d", i), func() {})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tf.Reset()
		goflowExec.Run(tf).Wait()
	}
}

func BenchmarkGoFlow_C1024(b *testing.B) {
	tf := NewTaskFlow("G")
	for i := 0; i < 1024; i++ {
		tf.NewTask(fmt.Sprintf("N%d", i), func() {})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tf.Reset()
		goflowExec.Run(tf).Wait()
	}
}

// --- Deep sequential chains ---

func BenchmarkGoFlow_S128(b *testing.B) {
	tf := NewTaskFlow("G")
	prev := tf.NewTask("N0", func() {})
	for i := 1; i < 128; i++ {
		next := tf.NewTask(fmt.Sprintf("N%d", i), func() {})
		prev.Precede(next)
		prev = next
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tf.Reset()
		goflowExec.Run(tf).Wait()
	}
}

// --- Fan-out / fan-in (1 -> N -> 1) ---

func benchGoFlowFanOutFanIn(b *testing.B, width int) {
	tf := NewTaskFlow("G")
	src := tf.NewTask("src", func() {})
	sink := tf.NewTask("sink", func() {})
	for i := 0; i < width; i++ {
		mid := tf.NewTask(fmt.Sprintf("M%d", i), func() {})
		src.Precede(mid)
		mid.Precede(sink)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tf.Reset()
		goflowExec.Run(tf).Wait()
	}
}

func BenchmarkGoFlow_FanOutFanIn_64(b *testing.B)  { benchGoFlowFanOutFanIn(b, 64) }
func BenchmarkGoFlow_FanOutFanIn_256(b *testing.B) { benchGoFlowFanOutFanIn(b, 256) }

// --- Pipeline stages (each stage has N parallel tasks) ---

func benchGoFlowPipeline(b *testing.B, stages, width int) {
	tf := NewTaskFlow("G")
	var prevLayer []*Task
	for s := 0; s < stages; s++ {
		var curLayer []*Task
		for w := 0; w < width; w++ {
			t := tf.NewTask(fmt.Sprintf("S%d_W%d", s, w), func() {})
			for _, p := range prevLayer {
				p.Precede(t)
			}
			curLayer = append(curLayer, t)
		}
		prevLayer = curLayer
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tf.Reset()
		goflowExec.Run(tf).Wait()
	}
}

func BenchmarkGoFlow_Pipeline_4x16(b *testing.B)  { benchGoFlowPipeline(b, 4, 16) }
func BenchmarkGoFlow_Pipeline_8x32(b *testing.B)  { benchGoFlowPipeline(b, 8, 32) }
func BenchmarkGoFlow_Pipeline_16x8(b *testing.B)  { benchGoFlowPipeline(b, 16, 8) }

// --- Binary tree DAG ---

func BenchmarkGoFlow_BinaryTree_depth6(b *testing.B) {
	tf := NewTaskFlow("G")
	var build func(depth int) *Task
	id := 0
	build = func(depth int) *Task {
		id++
		t := tf.NewTask(fmt.Sprintf("N%d", id), func() {})
		if depth > 0 {
			left := build(depth - 1)
			right := build(depth - 1)
			t.Precede(left, right)
		}
		return t
	}
	build(6) // 127 nodes
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tf.Reset()
		goflowExec.Run(tf).Wait()
	}
}

// --- Diamond chain (A->B,C->D repeated N times) ---

func BenchmarkGoFlow_DiamondChain_16(b *testing.B) {
	tf := NewTaskFlow("G")
	prev := tf.NewTask("start", func() {})
	for i := 0; i < 16; i++ {
		left := tf.NewTask(fmt.Sprintf("L%d", i), func() {})
		right := tf.NewTask(fmt.Sprintf("R%d", i), func() {})
		join := tf.NewTask(fmt.Sprintf("J%d", i), func() {})
		prev.Precede(left, right)
		join.Succeed(left, right)
		prev = join
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tf.Reset()
		goflowExec.Run(tf).Wait()
	}
}

// --- Subflow benchmarks (go-flow exclusive feature: dynamic re-instantiation) ---

func BenchmarkGoFlow_Subflow_Static(b *testing.B) {
	tf := NewTaskFlow("G")
	tf.NewSubflow("sf", func(sf *Subflow) {
		s1 := sf.NewTask("s1", func() {})
		s2 := sf.NewTask("s2", func() {})
		s3 := sf.NewTask("s3", func() {})
		s4 := sf.NewTask("s4", func() {})
		s1.Precede(s2, s3)
		s4.Succeed(s2, s3)
	})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tf.Reset()
		goflowExec.Run(tf).Wait()
	}
}

func BenchmarkGoFlow_Subflow_Dynamic_10(b *testing.B) {
	var counter atomic.Int32
	tf := NewTaskFlow("G")
	tf.NewSubflow("dsf", func(sf *Subflow) {
		n := int(counter.Add(1)) % 10
		if n == 0 {
			n = 10
		}
		for i := 0; i < n; i++ {
			sf.NewTask(fmt.Sprintf("d%d", i), func() {})
		}
	})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tf.Reset()
		goflowExec.Run(tf).Wait()
	}
}

func BenchmarkGoFlow_Subflow_Nested_3(b *testing.B) {
	tf := NewTaskFlow("G")
	tf.NewSubflow("L0", func(sf0 *Subflow) {
		sf0.NewSubflow("L1", func(sf1 *Subflow) {
			sf1.NewSubflow("L2", func(sf2 *Subflow) {
				sf2.NewTask("leaf", func() {})
			})
		})
	})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tf.Reset()
		goflowExec.Run(tf).Wait()
	}
}

func BenchmarkGoFlow_Subflow_Wide(b *testing.B) {
	tf := NewTaskFlow("G")
	for i := 0; i < 8; i++ {
		idx := i
		tf.NewSubflow(fmt.Sprintf("sf%d", idx), func(sf *Subflow) {
			for j := 0; j < 8; j++ {
				sf.NewTask(fmt.Sprintf("s%d_%d", idx, j), func() {})
			}
		})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tf.Reset()
		goflowExec.Run(tf).Wait()
	}
}

// --- Condition loop benchmarks ---

func benchGoFlowCondLoop(b *testing.B, iterations int) {
	tf := NewTaskFlow("G")
	var counter atomic.Int32
	init := tf.NewTask("init", func() { counter.Store(0) })
	body := tf.NewTask("body", func() { counter.Add(1) })
	done := tf.NewTask("done", func() {})
	limit := int32(iterations)
	cond := tf.NewCondition("cond", func() uint {
		if counter.Load() < limit {
			return 0
		}
		return 1
	})
	init.Precede(body)
	body.Precede(cond)
	cond.Precede(body, done)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tf.Reset()
		goflowExec.Run(tf).Wait()
	}
}

func BenchmarkGoFlow_CondLoop_10(b *testing.B)  { benchGoFlowCondLoop(b, 10) }
func BenchmarkGoFlow_CondLoop_100(b *testing.B) { benchGoFlowCondLoop(b, 100) }

// --- CPU-bound workload (measures framework overhead relative to actual work) ---

func cpuWork() {
	x := 1.0
	for j := 0; j < 1000; j++ {
		x = math.Sqrt(x + float64(j))
	}
	_ = x
}

func BenchmarkGoFlow_CPUBound_C32(b *testing.B) {
	tf := NewTaskFlow("G")
	for i := 0; i < 32; i++ {
		tf.NewTask(fmt.Sprintf("N%d", i), cpuWork)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tf.Reset()
		goflowExec.Run(tf).Wait()
	}
}

func BenchmarkGoFlow_CPUBound_Pipeline4x8(b *testing.B) {
	tf := NewTaskFlow("G")
	var prevLayer []*Task
	for s := 0; s < 4; s++ {
		var curLayer []*Task
		for w := 0; w < 8; w++ {
			t := tf.NewTask(fmt.Sprintf("S%d_W%d", s, w), cpuWork)
			for _, p := range prevLayer {
				p.Precede(t)
			}
			curLayer = append(curLayer, t)
		}
		prevLayer = curLayer
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tf.Reset()
		goflowExec.Run(tf).Wait()
	}
}

// --- Repeated execution (measures setup/reset cost) ---

func BenchmarkGoFlow_RepeatedRun_10(b *testing.B) {
	tf := NewTaskFlow("G")
	for i := 0; i < 16; i++ {
		tf.NewTask(fmt.Sprintf("N%d", i), func() {})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for r := 0; r < 10; r++ {
			tf.Reset()
			goflowExec.Run(tf).Wait()
		}
	}
}

// =====================================================================
// go-taskflow comparison benchmarks
// =====================================================================

var gtfExec = gtf.NewExecutor(6400)

func BenchmarkGoTaskflow_C32(b *testing.B) {
	tf := gtf.NewTaskFlow("G")
	for i := 0; i < 32; i++ {
		tf.NewTask(fmt.Sprintf("N%d", i), func() {})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gtfExec.Run(tf).Wait()
	}
}

func BenchmarkGoTaskflow_S32(b *testing.B) {
	tf := gtf.NewTaskFlow("G")
	prev := tf.NewTask("N0", func() {})
	for i := 1; i < 32; i++ {
		next := tf.NewTask(fmt.Sprintf("N%d", i), func() {})
		prev.Precede(next)
		prev = next
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gtfExec.Run(tf).Wait()
	}
}

func BenchmarkGoTaskflow_C6(b *testing.B) {
	tf := gtf.NewTaskFlow("G")
	n0 := tf.NewTask("N0", func() {})
	n1 := tf.NewTask("N1", func() {})
	n2 := tf.NewTask("N2", func() {})
	n3 := tf.NewTask("N3", func() {})
	n4 := tf.NewTask("N4", func() {})
	n5 := tf.NewTask("N5", func() {})
	n0.Precede(n1, n2)
	n1.Precede(n3)
	n2.Precede(n4)
	n5.Succeed(n3, n4)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gtfExec.Run(tf).Wait()
	}
}

func BenchmarkGoTaskflow_C8x8(b *testing.B) {
	tf := gtf.NewTaskFlow("G")
	layersCount := 8
	layerNodesCount := 8
	var curLayer, upperLayer []*gtf.Task
	for i := 0; i < layersCount; i++ {
		for j := 0; j < layerNodesCount; j++ {
			task := tf.NewTask(fmt.Sprintf("N%d", i*layersCount+j), func() {})
			for k := range upperLayer {
				upperLayer[k].Precede(task)
			}
			curLayer = append(curLayer, task)
		}
		upperLayer = curLayer
		curLayer = []*gtf.Task{}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gtfExec.Run(tf).Wait()
	}
}

func BenchmarkGoTaskflow_C256(b *testing.B) {
	tf := gtf.NewTaskFlow("G")
	for i := 0; i < 256; i++ {
		tf.NewTask(fmt.Sprintf("N%d", i), func() {})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gtfExec.Run(tf).Wait()
	}
}

func BenchmarkGoTaskflow_C1024(b *testing.B) {
	tf := gtf.NewTaskFlow("G")
	for i := 0; i < 1024; i++ {
		tf.NewTask(fmt.Sprintf("N%d", i), func() {})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gtfExec.Run(tf).Wait()
	}
}

func BenchmarkGoTaskflow_S128(b *testing.B) {
	tf := gtf.NewTaskFlow("G")
	prev := tf.NewTask("N0", func() {})
	for i := 1; i < 128; i++ {
		next := tf.NewTask(fmt.Sprintf("N%d", i), func() {})
		prev.Precede(next)
		prev = next
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gtfExec.Run(tf).Wait()
	}
}

func benchGoTaskflowFanOutFanIn(b *testing.B, width int) {
	tf := gtf.NewTaskFlow("G")
	src := tf.NewTask("src", func() {})
	sink := tf.NewTask("sink", func() {})
	for i := 0; i < width; i++ {
		mid := tf.NewTask(fmt.Sprintf("M%d", i), func() {})
		src.Precede(mid)
		mid.Precede(sink)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gtfExec.Run(tf).Wait()
	}
}

func BenchmarkGoTaskflow_FanOutFanIn_64(b *testing.B)  { benchGoTaskflowFanOutFanIn(b, 64) }
func BenchmarkGoTaskflow_FanOutFanIn_256(b *testing.B) { benchGoTaskflowFanOutFanIn(b, 256) }

func benchGoTaskflowPipeline(b *testing.B, stages, width int) {
	tf := gtf.NewTaskFlow("G")
	var prevLayer []*gtf.Task
	for s := 0; s < stages; s++ {
		var curLayer []*gtf.Task
		for w := 0; w < width; w++ {
			t := tf.NewTask(fmt.Sprintf("S%d_W%d", s, w), func() {})
			for _, p := range prevLayer {
				p.Precede(t)
			}
			curLayer = append(curLayer, t)
		}
		prevLayer = curLayer
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gtfExec.Run(tf).Wait()
	}
}

func BenchmarkGoTaskflow_Pipeline_4x16(b *testing.B)  { benchGoTaskflowPipeline(b, 4, 16) }
func BenchmarkGoTaskflow_Pipeline_8x32(b *testing.B)  { benchGoTaskflowPipeline(b, 8, 32) }
func BenchmarkGoTaskflow_Pipeline_16x8(b *testing.B)  { benchGoTaskflowPipeline(b, 16, 8) }

func BenchmarkGoTaskflow_BinaryTree_depth6(b *testing.B) {
	tf := gtf.NewTaskFlow("G")
	var build func(depth int) *gtf.Task
	id := 0
	build = func(depth int) *gtf.Task {
		id++
		t := tf.NewTask(fmt.Sprintf("N%d", id), func() {})
		if depth > 0 {
			left := build(depth - 1)
			right := build(depth - 1)
			t.Precede(left, right)
		}
		return t
	}
	build(6)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gtfExec.Run(tf).Wait()
	}
}

func BenchmarkGoTaskflow_DiamondChain_16(b *testing.B) {
	tf := gtf.NewTaskFlow("G")
	prev := tf.NewTask("start", func() {})
	for i := 0; i < 16; i++ {
		left := tf.NewTask(fmt.Sprintf("L%d", i), func() {})
		right := tf.NewTask(fmt.Sprintf("R%d", i), func() {})
		join := tf.NewTask(fmt.Sprintf("J%d", i), func() {})
		prev.Precede(left, right)
		join.Succeed(left, right)
		prev = join
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gtfExec.Run(tf).Wait()
	}
}

func BenchmarkGoTaskflow_CPUBound_C32(b *testing.B) {
	tf := gtf.NewTaskFlow("G")
	for i := 0; i < 32; i++ {
		tf.NewTask(fmt.Sprintf("N%d", i), cpuWork)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gtfExec.Run(tf).Wait()
	}
}

func BenchmarkGoTaskflow_CPUBound_Pipeline4x8(b *testing.B) {
	tf := gtf.NewTaskFlow("G")
	var prevLayer []*gtf.Task
	for s := 0; s < 4; s++ {
		var curLayer []*gtf.Task
		for w := 0; w < 8; w++ {
			t := tf.NewTask(fmt.Sprintf("S%d_W%d", s, w), cpuWork)
			for _, p := range prevLayer {
				p.Precede(t)
			}
			curLayer = append(curLayer, t)
		}
		prevLayer = curLayer
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gtfExec.Run(tf).Wait()
	}
}

func BenchmarkGoTaskflow_Subflow_Static(b *testing.B) {
	tf := gtf.NewTaskFlow("G")
	tf.NewSubflow("sf", func(sf *gtf.Subflow) {
		s1 := sf.NewTask("s1", func() {})
		s2 := sf.NewTask("s2", func() {})
		s3 := sf.NewTask("s3", func() {})
		s4 := sf.NewTask("s4", func() {})
		s1.Precede(s2, s3)
		s4.Succeed(s2, s3)
	})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gtfExec.Run(tf).Wait()
	}
}

func BenchmarkGoTaskflow_RepeatedRun_10(b *testing.B) {
	tf := gtf.NewTaskFlow("G")
	for i := 0; i < 16; i++ {
		tf.NewTask(fmt.Sprintf("N%d", i), func() {})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for r := 0; r < 10; r++ {
			gtfExec.Run(tf).Wait()
		}
	}
}
