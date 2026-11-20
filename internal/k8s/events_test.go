package k8s

import (
	"context"
	"reflect"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/emircanagac/northscope/internal/k8s/synthetic"
	"github.com/emircanagac/northscope/internal/models"
)

func trimmed[T any](t *testing.T, obj T) T {
	t.Helper()
	out, err := trimForTopology(obj)
	if err != nil {
		t.Fatalf("trim: %v", err)
	}
	return out.(T)
}

// The trimmed cache must produce exactly the topology the full objects do,
// including Pods that are not ready, crash looping, or terminating.
func TestTrimmedObjectsBuildTheSameTopology(t *testing.T) {
	cluster := synthetic.New(synthetic.Spec{Namespaces: 3, ServicesPerNamespace: 3, PodsPerService: 3, Nodes: 3})
	cluster.Pods[3].Status.Conditions[1] = corev1.PodCondition{Type: corev1.PodReady, Status: corev1.ConditionFalse, Reason: "ContainersNotReady", Message: "containers with unready status: [app]"}
	cluster.Pods[4].Status.ContainerStatuses[0].State = corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff", Message: "back-off 5m0s"}}
	cluster.Pods[5].Status.ContainerStatuses[0].State = corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: "OOMKilled", ExitCode: 137}}
	deleting := metav1.Now()
	cluster.Pods[6].DeletionTimestamp = &deleting
	cluster.Nodes[1].Status.Conditions[2].Status = corev1.ConditionFalse

	full := syntheticInputsFrom(cluster)
	trimmedInputs := full
	trimmedInputs.pods = nil
	for _, pod := range cluster.Pods {
		trimmedInputs.pods = append(trimmedInputs.pods, trimmed(t, pod.DeepCopy()))
	}
	trimmedInputs.nodes = nil
	for _, node := range cluster.Nodes {
		trimmedInputs.nodes = append(trimmedInputs.nodes, trimmed(t, node.DeepCopy()))
	}
	trimmedInputs.endpointSlices = nil
	for _, slice := range cluster.EndpointSlices {
		trimmedInputs.endpointSlices = append(trimmedInputs.endpointSlices, trimmed(t, slice.DeepCopy()))
	}

	want := snapshotFromInputs("", full)
	got := snapshotFromInputs("", trimmedInputs)
	if !reflect.DeepEqual(want.Nodes, got.Nodes) || !reflect.DeepEqual(want.Edges, got.Edges) || want.Inventory != got.Inventory {
		t.Fatal("expected trimmed objects to build the same topology as full objects")
	}
}

func syntheticInputsFrom(cluster synthetic.Cluster) snapshotInputs {
	return snapshotInputs{
		ingresses:      cluster.Ingresses,
		ingressClasses: cluster.IngressClasses,
		services:       cluster.Services,
		endpointSlices: cluster.EndpointSlices,
		pods:           cluster.Pods,
		nodes:          cluster.Nodes,
	}
}

func TestTrimForTopologyDropsUnusedFieldsAndIsIdempotent(t *testing.T) {
	pod := synthetic.Pod("demo", "web-1", map[string]string{"app": "web"}, "worker-1", "10.0.0.5")
	pod.ResourceVersion = "42"
	once := trimmed(t, pod)
	if len(once.Spec.Containers) != 0 || once.Annotations != nil || once.OwnerReferences != nil || once.Status.HostIP != "" {
		t.Fatalf("expected the Pod spec, annotations, owners, and host IP to be dropped, got %+v", once)
	}
	if once.Name != "web-1" || once.Labels["app"] != "web" || once.Spec.NodeName != "worker-1" || once.Status.PodIP != "10.0.0.5" || once.ResourceVersion != "42" {
		t.Fatalf("expected identity, labels, node, IP, and resource version to be kept, got %+v", once)
	}
	if twice := trimmed(t, once); !reflect.DeepEqual(once, twice) {
		t.Fatal("expected trimming a trimmed Pod to change nothing")
	}

	node := trimmed(t, synthetic.Node("worker-1"))
	if len(node.Status.Images) != 0 || len(node.Status.Addresses) != 0 || len(node.Status.Conditions) != 1 || node.Status.Conditions[0].Type != corev1.NodeReady {
		t.Fatalf("expected only the Ready condition and no images or addresses, got %+v", node.Status)
	}
	if node.Status.NodeInfo.KubeletVersion == "" || node.Labels["kubernetes.io/hostname"] != "worker-1" {
		t.Fatalf("expected kubelet version and labels to be kept, got %+v", node)
	}
}

func TestTopologyRelevantUpdate(t *testing.T) {
	pod := trimmed(t, synthetic.Pod("demo", "web-1", map[string]string{"app": "web"}, "worker-1", "10.0.0.5"))
	pod.ResourceVersion = "1"

	resync := pod.DeepCopy()
	if topologyRelevantUpdate(pod, resync) {
		t.Fatal("expected a resync of the same resource version to be ignored")
	}

	// A restart count or probe time change leaves the trimmed Pod equal.
	restarted := synthetic.Pod("demo", "web-1", map[string]string{"app": "web"}, "worker-1", "10.0.0.5")
	restarted.Status.ContainerStatuses[0].RestartCount = 3
	restarted.Status.Conditions[1].LastProbeTime = metav1.Now()
	restarted.ResourceVersion = "2"
	if topologyRelevantUpdate(pod, trimmed(t, restarted)) {
		t.Fatal("expected an update of dropped Pod fields to be ignored")
	}

	relabeled := pod.DeepCopy()
	relabeled.ResourceVersion = "3"
	relabeled.Labels = map[string]string{"app": "api"}
	if !topologyRelevantUpdate(pod, relabeled) {
		t.Fatal("expected a label change to be relevant")
	}

	notReady := pod.DeepCopy()
	notReady.ResourceVersion = "4"
	notReady.Status.Conditions[1].Status = corev1.ConditionFalse
	if !topologyRelevantUpdate(pod, notReady) {
		t.Fatal("expected a readiness change to be relevant")
	}

	node := trimmed(t, synthetic.Node("worker-1"))
	node.ResourceVersion = "1"
	heartbeat := synthetic.Node("worker-1")
	heartbeat.ResourceVersion = "2"
	heartbeat.Status.Conditions[2].LastHeartbeatTime = metav1.Now()
	if topologyRelevantUpdate(node, trimmed(t, heartbeat)) {
		t.Fatal("expected a Node heartbeat to be ignored")
	}

	// Control planes before Kubernetes 1.24 renew leader election through
	// Endpoints annotations every few seconds.
	endpoints := trimmed(t, &corev1.Endpoints{ObjectMeta: metav1.ObjectMeta{Namespace: "kube-system", Name: "kube-scheduler", ResourceVersion: "1"}})
	renewed := &corev1.Endpoints{ObjectMeta: metav1.ObjectMeta{
		Namespace: "kube-system", Name: "kube-scheduler", ResourceVersion: "2",
		Annotations: map[string]string{"control-plane.alpha.kubernetes.io/leader": `{"renewTime":"2026-11-20T10:00:00Z"}`},
	}}
	if topologyRelevantUpdate(endpoints, trimmed(t, renewed)) {
		t.Fatal("expected an Endpoints leader-election renewal to be ignored")
	}

	slice := trimmed(t, synthetic.EndpointSlice(&corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Namespace: "demo", Name: "web"},
		Spec:       corev1.ServiceSpec{Ports: []corev1.ServicePort{{Name: "http", Port: 80}}},
	}, []*corev1.Pod{pod}))
	slice.ResourceVersion = "1"
	changed := slice.DeepCopy()
	changed.ResourceVersion = "2"
	unready := false
	changed.Endpoints[0].Conditions.Ready = &unready
	if !topologyRelevantUpdate(slice, changed) {
		t.Fatal("expected an endpoint readiness change to be relevant")
	}
	retriggered := slice.DeepCopy()
	retriggered.ResourceVersion = "3"
	retriggered.Annotations = map[string]string{"endpoints.kubernetes.io/last-change-trigger-time": "2026-11-20T10:00:00Z"}
	if topologyRelevantUpdate(slice, trimmed(t, retriggered)) {
		t.Fatal("expected an EndpointSlice annotation change to be ignored")
	}

	service := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "web", ResourceVersion: "1"}}
	annotated := service.DeepCopy()
	annotated.ResourceVersion = "2"
	annotated.Annotations = map[string]string{"example.test/owner": "team-a"}
	if !topologyRelevantUpdate(service, annotated) {
		t.Fatal("expected Service updates to stay relevant, because controller matching reads annotations")
	}
}

func TestIgnoredUpdatesDoNotRebuild(t *testing.T) {
	pod := synthetic.Pod("demo", "web-1", map[string]string{"app": "web"}, "worker-1", "10.0.0.5")
	client := fake.NewSimpleClientset(pod)
	watcher, err := NewWatcherFromClient(client, time.Minute)
	if err != nil {
		t.Fatalf("create watcher: %v", err)
	}
	watcher.rebuildDebounce = 0
	runWatcher(t, watcher)
	eventually(t, 5*time.Second, "first snapshot", watcher.Ready)
	builds := watcher.Metrics().SnapshotBuildsTotal

	restarted := pod.DeepCopy()
	restarted.Status.ContainerStatuses[0].RestartCount = 1
	if _, err := client.CoreV1().Pods("demo").UpdateStatus(context.Background(), restarted, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("update pod: %v", err)
	}
	eventually(t, 5*time.Second, "the ignored update to be counted", func() bool {
		return podEvents(watcher.Metrics().WatchEvents).Ignored == 1
	})

	relabeled := restarted.DeepCopy()
	relabeled.Labels = map[string]string{"app": "api"}
	if _, err := client.CoreV1().Pods("demo").Update(context.Background(), relabeled, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("update pod: %v", err)
	}
	eventually(t, 5*time.Second, "the relevant update to rebuild", func() bool {
		return podEvents(watcher.Metrics().WatchEvents).Updated == 1 && watcher.Metrics().SnapshotBuildsTotal == builds+1
	})
}

func podEvents(events []WatchEvents) WatchEvents {
	for _, item := range events {
		if item.Resource == "pods" {
			return item
		}
	}
	return WatchEvents{}
}

func TestRebuildRunsWithinMaxDelayDuringContinuousChanges(t *testing.T) {
	watcher, err := NewWatcherFromClient(fake.NewSimpleClientset(), time.Hour)
	if err != nil {
		t.Fatalf("create watcher: %v", err)
	}
	watcher.rebuildDebounce = 100 * time.Millisecond
	watcher.maxRebuildDelay = 300 * time.Millisecond
	runWatcher(t, watcher)
	eventually(t, 5*time.Second, "first snapshot", watcher.Ready)
	builds := watcher.Metrics().SnapshotBuildsTotal

	// A change every 20ms keeps restarting the 100ms debounce.
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				watcher.rebuildAndPublishWhenReady()
			}
		}
	}()
	eventually(t, 2*time.Second, "a rebuild despite continuous changes", func() bool {
		return watcher.Metrics().SnapshotBuildsTotal > builds
	})
}

func TestWatcherPublishesDeltasBetweenConsecutiveVersions(t *testing.T) {
	watcher, err := NewWatcherFromClient(fake.NewSimpleClientset(), time.Minute)
	if err != nil {
		t.Fatalf("create watcher: %v", err)
	}
	inputs := syntheticInputs(synthetic.Small)
	watcher.buildSnapshotFunc = func() (models.TopologySnapshot, error) {
		return snapshotFromInputs("", inputs), nil
	}
	watcher.rebuildAndPublish()
	updates, unsubscribe := watcher.Subscribe(4)
	defer unsubscribe()
	first := <-updates
	if first.Delta != nil {
		t.Fatal("expected the first publication to a subscriber to be a full snapshot")
	}

	pods := append([]*corev1.Pod(nil), inputs.pods...)
	notReady := pods[5].DeepCopy()
	notReady.Status.Conditions[1].Status = corev1.ConditionFalse
	pods[5] = notReady
	inputs.pods = pods
	watcher.rebuildAndPublish()

	second := <-updates
	if second.Delta == nil {
		t.Fatal("expected a delta for a one-Pod change")
	}
	if second.Delta.BaseVersion != first.Snapshot.Version || second.Delta.Version != second.Snapshot.Version {
		t.Fatalf("expected delta v%d->v%d, got v%d->v%d", first.Snapshot.Version, second.Snapshot.Version, second.Delta.BaseVersion, second.Delta.Version)
	}
	if applied := applyDelta(first.Snapshot, second.Delta); !sameTopology(applied, second.Snapshot) {
		t.Fatal("expected the delta to turn the previous snapshot into the new one")
	}
	if metrics := watcher.Metrics(); metrics.SnapshotDeltasTotal != 1 || metrics.SnapshotBuildDurations.Count != 2 {
		t.Fatalf("expected one delta and two observed builds, got %+v", metrics)
	}
}

// applyDelta is the reference implementation of TopologyDelta that the UI
// mirrors in topologyDelta.ts.
func applyDelta(base models.TopologySnapshot, delta *models.TopologyDelta) models.TopologySnapshot {
	nodes := map[string]models.Node{}
	for _, node := range base.Nodes {
		nodes[node.ID] = node
	}
	for _, id := range delta.RemoveNodes {
		delete(nodes, id)
	}
	for _, node := range delta.UpsertNodes {
		nodes[node.ID] = node
	}
	edges := map[string]models.Edge{}
	for _, edge := range base.Edges {
		edges[edge.ID] = edge
	}
	for _, id := range delta.RemoveEdges {
		delete(edges, id)
	}
	for _, edge := range delta.UpsertEdges {
		edges[edge.ID] = edge
	}
	result := models.TopologySnapshot{
		Instance: delta.Instance, Version: delta.Version, GeneratedAt: delta.GeneratedAt,
		Inventory: delta.Inventory, Health: delta.Health,
	}
	for _, node := range nodes {
		result.Nodes = append(result.Nodes, node)
	}
	for _, edge := range edges {
		result.Edges = append(result.Edges, edge)
	}
	return result
}

func TestTopologyDelta(t *testing.T) {
	base := snapshotFromInputs("", syntheticInputs(synthetic.Small))
	baseIndex := indexTopology(base)

	if delta := topologyDelta(baseIndex, baseIndex, base, 1); delta == nil || len(delta.UpsertNodes)+len(delta.RemoveNodes)+len(delta.UpsertEdges)+len(delta.RemoveEdges) != 0 {
		t.Fatalf("expected an empty delta for a health-only change, got %+v", delta)
	}

	next := base
	next.Nodes = append([]models.Node(nil), base.Nodes...)
	next.Nodes[0].Data.Status = "Changed"
	next.Nodes = next.Nodes[:len(next.Nodes)-1]
	next.Nodes[1].Position.Y += 140 // moved in the default grid only
	delta := topologyDelta(baseIndex, indexTopology(next), next, 1)
	if delta == nil || len(delta.UpsertNodes) != 1 || delta.UpsertNodes[0].ID != base.Nodes[0].ID {
		t.Fatalf("expected exactly the changed node to be sent, got %+v", delta)
	}
	if len(delta.RemoveNodes) != 1 || delta.RemoveNodes[0] != base.Nodes[len(base.Nodes)-1].ID {
		t.Fatalf("expected the dropped node to be removed, got %v", delta.RemoveNodes)
	}

	if delta := topologyDelta(baseIndex, indexTopology(models.TopologySnapshot{}), models.TopologySnapshot{}, 1); delta != nil {
		t.Fatal("expected no delta when most of the topology changed")
	}
}

func TestIndexTopologyIgnoresMapOrderButNotContent(t *testing.T) {
	node := models.Node{ID: "pod:a", Data: models.NodeData{Metadata: map[string]string{"a": "1", "b": "2"}}}
	same := models.Node{ID: "pod:a", Data: models.NodeData{Metadata: map[string]string{"b": "2", "a": "1"}}}
	swapped := models.Node{ID: "pod:a", Data: models.NodeData{Metadata: map[string]string{"a": "2", "b": "1"}}}
	moved := models.Node{ID: "pod:a", Data: models.NodeData{Metadata: map[string]string{"ab": "", "": "12"}}}
	var f fingerprint
	if f.node(&node) != f.node(&same) {
		t.Fatal("expected label order not to matter")
	}
	if f.node(&node) == f.node(&swapped) || f.node(&node) == f.node(&moved) {
		t.Fatal("expected different labels to fingerprint differently")
	}
	edge := models.Edge{ID: "e", Data: &models.EdgeData{Kind: "routes"}}
	if f.edge(&edge) == f.edge(&models.Edge{ID: "e"}) {
		t.Fatal("expected an edge without data to differ from one with data")
	}
}
