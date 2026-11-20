package k8s

import (
	"sort"
	"sync"
	"sync/atomic"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	discoveryv1beta1 "k8s.io/api/discovery/v1beta1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
)

// trimForTopology drops the parts of cached objects that topology builds
// never read, before they enter informer caches. On large clusters Pods and
// Nodes dominate NorthScope's memory: Pod specs (containers, env, volumes),
// Node image lists, and annotations are most of their size.
//
// Trimming also keeps fields that change on their own out of the cache, such
// as condition heartbeat times, the Endpoints leader-election annotations of
// older control planes, and Endpoints target resource versions, so updates
// that only touch them can be ignored (see topologyRelevantUpdate).
//
// The transform must stay idempotent: client-go may apply it to an object it
// already transformed.
func trimForTopology(obj interface{}) (interface{}, error) {
	switch item := obj.(type) {
	case *corev1.Pod:
		return trimPod(item), nil
	case *corev1.Node:
		return trimNode(item), nil
	case *corev1.Endpoints:
		item.ManagedFields = nil
		item.Annotations = nil
		for i := range item.Subsets {
			clearTargetRefVersions(item.Subsets[i].Addresses)
			clearTargetRefVersions(item.Subsets[i].NotReadyAddresses)
		}
		return item, nil
	case *discoveryv1.EndpointSlice:
		item.ManagedFields = nil
		item.Annotations = nil
		for i := range item.Endpoints {
			if ref := item.Endpoints[i].TargetRef; ref != nil {
				ref.ResourceVersion = ""
			}
		}
		return item, nil
	case *discoveryv1beta1.EndpointSlice:
		item.ManagedFields = nil
		item.Annotations = nil
		for i := range item.Endpoints {
			if ref := item.Endpoints[i].TargetRef; ref != nil {
				ref.ResourceVersion = ""
			}
		}
		return item, nil
	}
	return stripManagedFields(obj)
}

// trimPod keeps what the builder reads: identity, labels, deletion, node,
// IP, phase, condition reasons, and container waiting or terminated reasons.
func trimPod(pod *corev1.Pod) *corev1.Pod {
	trimmed := &corev1.Pod{
		TypeMeta: pod.TypeMeta,
		ObjectMeta: metav1.ObjectMeta{
			Name:              pod.Name,
			Namespace:         pod.Namespace,
			UID:               pod.UID,
			ResourceVersion:   pod.ResourceVersion,
			CreationTimestamp: pod.CreationTimestamp,
			DeletionTimestamp: pod.DeletionTimestamp,
			Labels:            pod.Labels,
		},
		Spec: corev1.PodSpec{NodeName: pod.Spec.NodeName},
		Status: corev1.PodStatus{
			Phase: pod.Status.Phase,
			PodIP: pod.Status.PodIP,
		},
	}
	for _, condition := range pod.Status.Conditions {
		trimmed.Status.Conditions = append(trimmed.Status.Conditions, corev1.PodCondition{
			Type:   condition.Type,
			Status: condition.Status,
			Reason: condition.Reason,
		})
	}
	for _, status := range pod.Status.ContainerStatuses {
		state := corev1.ContainerState{}
		if status.State.Waiting != nil {
			state.Waiting = &corev1.ContainerStateWaiting{Reason: status.State.Waiting.Reason}
		}
		if status.State.Terminated != nil {
			state.Terminated = &corev1.ContainerStateTerminated{Reason: status.State.Terminated.Reason}
		}
		if status.State.Running != nil {
			state.Running = &corev1.ContainerStateRunning{}
		}
		trimmed.Status.ContainerStatuses = append(trimmed.Status.ContainerStatuses, corev1.ContainerStatus{
			Name:  status.Name,
			Ready: status.Ready,
			State: state,
		})
	}
	return trimmed
}

// trimNode keeps the name, labels, kubelet version, OS image, and whether the
// node is Ready.
func trimNode(node *corev1.Node) *corev1.Node {
	trimmed := &corev1.Node{
		TypeMeta: node.TypeMeta,
		ObjectMeta: metav1.ObjectMeta{
			Name:              node.Name,
			UID:               node.UID,
			ResourceVersion:   node.ResourceVersion,
			CreationTimestamp: node.CreationTimestamp,
			DeletionTimestamp: node.DeletionTimestamp,
			Labels:            node.Labels,
		},
		Status: corev1.NodeStatus{NodeInfo: corev1.NodeSystemInfo{
			KubeletVersion: node.Status.NodeInfo.KubeletVersion,
			OSImage:        node.Status.NodeInfo.OSImage,
		}},
	}
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeReady {
			trimmed.Status.Conditions = []corev1.NodeCondition{{Type: condition.Type, Status: condition.Status}}
			break
		}
	}
	return trimmed
}

func clearTargetRefVersions(addresses []corev1.EndpointAddress) {
	for i := range addresses {
		if ref := addresses[i].TargetRef; ref != nil {
			ref.ResourceVersion = ""
		}
	}
}

// topologyRelevantUpdate reports whether an update can change the topology.
// Informer resyncs deliver the same object again, and for trimmed Pods, Nodes,
// Endpoints, and EndpointSlices an update that only changed fields NorthScope
// drops (status heartbeats, restart counts, annotations) leaves the trimmed
// objects equal apart from their resource version.
func topologyRelevantUpdate(oldObj, newObj interface{}) bool {
	oldMeta, oldErr := meta.Accessor(oldObj)
	newMeta, newErr := meta.Accessor(newObj)
	if oldErr != nil || newErr != nil {
		return true
	}
	if oldMeta.GetResourceVersion() != "" && oldMeta.GetResourceVersion() == newMeta.GetResourceVersion() {
		return false
	}
	switch old := oldObj.(type) {
	case *corev1.Pod:
		if updated, ok := newObj.(*corev1.Pod); ok {
			copied := *old
			copied.ResourceVersion = updated.ResourceVersion
			return !equality.Semantic.DeepEqual(&copied, updated)
		}
	case *corev1.Node:
		if updated, ok := newObj.(*corev1.Node); ok {
			copied := *old
			copied.ResourceVersion = updated.ResourceVersion
			return !equality.Semantic.DeepEqual(&copied, updated)
		}
	case *corev1.Endpoints:
		if updated, ok := newObj.(*corev1.Endpoints); ok {
			copied := *old
			copied.ResourceVersion = updated.ResourceVersion
			return !equality.Semantic.DeepEqual(&copied, updated)
		}
	case *discoveryv1.EndpointSlice:
		if updated, ok := newObj.(*discoveryv1.EndpointSlice); ok {
			copied := *old
			copied.ResourceVersion = updated.ResourceVersion
			return !equality.Semantic.DeepEqual(&copied, updated)
		}
	case *discoveryv1beta1.EndpointSlice:
		if updated, ok := newObj.(*discoveryv1beta1.EndpointSlice); ok {
			copied := *old
			copied.ResourceVersion = updated.ResourceVersion
			return !equality.Semantic.DeepEqual(&copied, updated)
		}
	}
	return true
}

// WatchEvents counts informer events for one resource.
type WatchEvents struct {
	Resource string
	Added    uint64
	Updated  uint64
	Deleted  uint64
	// Ignored counts updates that could not change the topology, including
	// periodic resyncs, so they triggered no rebuild.
	Ignored uint64
}

type eventCounts struct {
	added, updated, deleted, ignored atomic.Uint64
}

type eventCounters struct {
	mu        sync.Mutex
	resources map[string]*eventCounts
}

func (c *eventCounters) forResource(resource string) *eventCounts {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.resources == nil {
		c.resources = map[string]*eventCounts{}
	}
	counts, ok := c.resources[resource]
	if !ok {
		counts = &eventCounts{}
		c.resources[resource] = counts
	}
	return counts
}

func (c *eventCounters) snapshot() []WatchEvents {
	c.mu.Lock()
	defer c.mu.Unlock()
	events := make([]WatchEvents, 0, len(c.resources))
	for resource, counts := range c.resources {
		events = append(events, WatchEvents{
			Resource: resource,
			Added:    counts.added.Load(),
			Updated:  counts.updated.Load(),
			Deleted:  counts.deleted.Load(),
			Ignored:  counts.ignored.Load(),
		})
	}
	sort.Slice(events, func(i, j int) bool { return events[i].Resource < events[j].Resource })
	return events
}

// eventHandler counts events for resource and requests a rebuild for every
// add, delete, and relevant update.
func (w *Watcher) eventHandler(resource string) cache.ResourceEventHandler {
	counts := w.events.forResource(resource)
	return cache.ResourceEventHandlerFuncs{
		AddFunc: func(interface{}) {
			counts.added.Add(1)
			w.rebuildAndPublishWhenReady()
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			if !topologyRelevantUpdate(oldObj, newObj) {
				counts.ignored.Add(1)
				return
			}
			counts.updated.Add(1)
			w.rebuildAndPublishWhenReady()
		},
		DeleteFunc: func(interface{}) {
			counts.deleted.Add(1)
			w.rebuildAndPublishWhenReady()
		},
	}
}
