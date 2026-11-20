package k8s

import "time"

// snapshotBuildBuckets are the upper bounds, in seconds, of the snapshot build
// duration histogram. Builds take milliseconds on small clusters and around a
// tenth of a second at 20,000 Pods.
var snapshotBuildBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

// Histogram is a Prometheus-style histogram: Counts[i] is the number of
// observations at or below Bounds[i], and Count includes the ones above the
// last bound.
type Histogram struct {
	Bounds []float64
	Counts []uint64
	Count  uint64
	Sum    float64
}

type durationHistogram struct {
	counts []uint64
	count  uint64
	sum    float64
}

// observe records one duration. Callers hold the watcher lock.
func (h *durationHistogram) observe(duration time.Duration) {
	if h.counts == nil {
		h.counts = make([]uint64, len(snapshotBuildBuckets))
	}
	seconds := duration.Seconds()
	for i, bound := range snapshotBuildBuckets {
		if seconds <= bound {
			h.counts[i]++
		}
	}
	h.count++
	h.sum += seconds
}

func (h *durationHistogram) snapshot() Histogram {
	counts := make([]uint64, len(snapshotBuildBuckets))
	copy(counts, h.counts)
	return Histogram{Bounds: snapshotBuildBuckets, Counts: counts, Count: h.count, Sum: h.sum}
}
