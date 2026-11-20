package k8s

import (
	"encoding/json"
	"fmt"
	"runtime"
	"testing"

	corev1 "k8s.io/api/core/v1"

	"github.com/emircanagac/northscope/internal/k8s/synthetic"
	"github.com/emircanagac/northscope/internal/models"
)

// Run with:
//
//	go test ./internal/k8s -run '^$' -bench . -benchmem
//
// The results for the release builds are recorded in
// docs/production-access.md under Capacity Guidance.

var benchmarkSizes = []struct {
	name string
	spec synthetic.Spec
}{
	{"small", synthetic.Small},
	{"medium", synthetic.Medium},
	{"large", synthetic.Large},
}

func syntheticInputs(spec synthetic.Spec) snapshotInputs {
	cluster := synthetic.New(spec)
	return snapshotInputs{
		ingresses:      cluster.Ingresses,
		ingressClasses: cluster.IngressClasses,
		services:       cluster.Services,
		endpointSlices: cluster.EndpointSlices,
		pods:           cluster.Pods,
		nodes:          cluster.Nodes,
	}
}

// BenchmarkSnapshotBuild measures one full snapshot build from cached objects,
// which runs after every relevant change (debounced by 250ms).
func BenchmarkSnapshotBuild(b *testing.B) {
	for _, size := range benchmarkSizes {
		b.Run(size.name, func(b *testing.B) {
			inputs := syntheticInputs(size.spec)
			var snapshot models.TopologySnapshot
			b.ReportAllocs()
			for b.Loop() {
				snapshot = snapshotFromInputs("", inputs)
			}
			encoded, err := json.Marshal(snapshot)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportMetric(float64(size.spec.Pods()), "pods")
			b.ReportMetric(float64(len(snapshot.Nodes)), "nodes")
			b.ReportMetric(float64(len(encoded)), "json-bytes")
		})
	}
}

// BenchmarkSnapshotUnchanged measures deciding that a rebuilt snapshot equals
// the published one, which is the common case after unrelated updates: the
// watcher fingerprints the rebuilt snapshot and compares it with the index
// it kept for the published one.
func BenchmarkSnapshotUnchanged(b *testing.B) {
	for _, size := range benchmarkSizes {
		b.Run(size.name, func(b *testing.B) {
			inputs := syntheticInputs(size.spec)
			published := indexTopology(snapshotFromInputs("", inputs))
			rebuilt := snapshotFromInputs("", inputs)
			b.ReportAllocs()
			for b.Loop() {
				if !indexTopology(rebuilt).equal(published) {
					b.Fatal("expected identical snapshots")
				}
			}
		})
	}
}

// BenchmarkSnapshotDelta measures publishing a change to one Pod: computing
// the delta and its JSON size compared with the full snapshot.
func BenchmarkSnapshotDelta(b *testing.B) {
	for _, size := range benchmarkSizes {
		b.Run(size.name, func(b *testing.B) {
			inputs := syntheticInputs(size.spec)
			base := snapshotFromInputs("", inputs)
			baseIndex := indexTopology(base)
			pods := append([]*corev1.Pod(nil), inputs.pods...)
			changed := pods[len(pods)/2].DeepCopy()
			changed.Status.Conditions[1].Status = corev1.ConditionFalse
			pods[len(pods)/2] = changed
			inputs.pods = pods
			next := snapshotFromInputs("", inputs)
			nextIndex := indexTopology(next)

			var delta *models.TopologyDelta
			b.ReportAllocs()
			for b.Loop() {
				delta = topologyDelta(baseIndex, nextIndex, next, 1)
			}
			if delta == nil {
				b.Fatal("expected a delta")
			}
			deltaJSON, err := json.Marshal(delta)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportMetric(float64(len(deltaJSON)), "delta-json-bytes")
		})
	}
}

// BenchmarkCachedObjectMemory reports the heap a cached Pod and Node take
// before and after trimForTopology. Real objects usually carry more (longer
// specs, more env and volumes), so the saving in a cluster is at least this.
func BenchmarkCachedObjectMemory(b *testing.B) {
	const count = 2000
	measure := func(build func(i int) any) float64 {
		keep := make([]any, count)
		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		for i := range keep {
			keep[i] = build(i)
		}
		runtime.GC()
		runtime.ReadMemStats(&after)
		runtime.KeepAlive(keep)
		return float64(after.HeapAlloc-before.HeapAlloc) / count
	}
	pod := func(i int) *corev1.Pod {
		return synthetic.Pod("team-001", fmt.Sprintf("svc-01-7c9f8d6b5-%05d", i), map[string]string{"app": "svc-01"}, "worker-001", "10.64.0.1")
	}
	node := func(i int) *corev1.Node { return synthetic.Node(fmt.Sprintf("worker-%04d", i)) }
	for b.Loop() {
		b.ReportMetric(measure(func(i int) any { return pod(i) }), "pod-bytes")
		b.ReportMetric(measure(func(i int) any { return trimPod(pod(i)) }), "trimmed-pod-bytes")
		b.ReportMetric(measure(func(i int) any { return node(i) }), "node-bytes")
		b.ReportMetric(measure(func(i int) any { return trimNode(node(i)) }), "trimmed-node-bytes")
	}
}
