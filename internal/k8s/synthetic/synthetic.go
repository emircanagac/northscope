// Package synthetic generates Kubernetes objects that look like a typical
// ingress-nginx cluster, sized by a Spec. Benchmarks use it to measure
// snapshot builds at a known scale, and the demo server uses it to run the UI
// without a cluster.
package synthetic

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// ControllerNamespace holds the ingress-nginx controller Service and Pods.
const ControllerNamespace = "ingress-nginx"

// Spec sizes a synthetic cluster. Every application namespace has one Ingress
// with a host per Service, and every Service selects its own Pods.
type Spec struct {
	Namespaces           int
	ServicesPerNamespace int
	PodsPerService       int
	Nodes                int
}

// Small, Medium, and Large are the sizes the benchmarks report.
var (
	Small  = Spec{Namespaces: 10, ServicesPerNamespace: 5, PodsPerService: 2, Nodes: 5}
	Medium = Spec{Namespaces: 50, ServicesPerNamespace: 10, PodsPerService: 4, Nodes: 50}
	Large  = Spec{Namespaces: 200, ServicesPerNamespace: 10, PodsPerService: 10, Nodes: 200}
)

// Pods is the number of Pods the spec generates, including the controller.
func (s Spec) Pods() int {
	return s.Namespaces*s.ServicesPerNamespace*s.PodsPerService + controllerReplicas
}

func (s Spec) String() string {
	return fmt.Sprintf("%dns-%dsvc-%dpods", s.Namespaces, s.Namespaces*s.ServicesPerNamespace, s.Pods())
}

const controllerReplicas = 2

// Cluster is a generated set of objects.
type Cluster struct {
	IngressClasses []*networkingv1.IngressClass
	Ingresses      []*networkingv1.Ingress
	Services       []*corev1.Service
	Pods           []*corev1.Pod
	Nodes          []*corev1.Node
	EndpointSlices []*discoveryv1.EndpointSlice
}

// New generates a cluster. The output is deterministic for a spec.
func New(spec Spec) Cluster {
	if spec.Nodes < 1 {
		spec.Nodes = 1
	}
	var cluster Cluster
	for i := range spec.Nodes {
		cluster.Nodes = append(cluster.Nodes, Node(fmt.Sprintf("worker-%03d", i)))
	}

	className := "nginx"
	cluster.IngressClasses = append(cluster.IngressClasses, &networkingv1.IngressClass{
		ObjectMeta: metav1.ObjectMeta{Name: className},
		Spec:       networkingv1.IngressClassSpec{Controller: "k8s.io/ingress-nginx"},
	})

	podIndex := 0
	nextPod := func(namespace, app string, replica int) *corev1.Pod {
		node := cluster.Nodes[podIndex%len(cluster.Nodes)].Name
		pod := Pod(namespace, fmt.Sprintf("%s-7c9f8d6b5-%05d", app, replica), map[string]string{"app": app}, node, podIP(podIndex))
		podIndex++
		return pod
	}

	controllerLabels := map[string]string{
		"app.kubernetes.io/name":      "ingress-nginx",
		"app.kubernetes.io/component": "controller",
	}
	controller := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Namespace: ControllerNamespace, Name: "ingress-nginx-controller", Labels: controllerLabels},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeLoadBalancer,
			Selector: controllerLabels,
			Ports: []corev1.ServicePort{
				{Name: "http", Port: 80, NodePort: 30080, Protocol: corev1.ProtocolTCP, TargetPort: intstr.FromString("http")},
				{Name: "https", Port: 443, NodePort: 30443, Protocol: corev1.ProtocolTCP, TargetPort: intstr.FromString("https")},
			},
		},
		Status: corev1.ServiceStatus{LoadBalancer: corev1.LoadBalancerStatus{
			Ingress: []corev1.LoadBalancerIngress{{IP: "203.0.113.10"}},
		}},
	}
	cluster.Services = append(cluster.Services, controller)
	var controllerPods []*corev1.Pod
	for replica := range controllerReplicas {
		pod := nextPod(ControllerNamespace, "ingress-nginx-controller", replica)
		pod.Labels = controllerLabels
		controllerPods = append(controllerPods, pod)
	}
	cluster.Pods = append(cluster.Pods, controllerPods...)
	cluster.EndpointSlices = append(cluster.EndpointSlices, EndpointSlice(controller, controllerPods))

	for n := range spec.Namespaces {
		namespace := fmt.Sprintf("team-%03d", n)
		ingress := &networkingv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "web", CreationTimestamp: metav1.Unix(1_700_000_000, 0)},
			Spec:       networkingv1.IngressSpec{IngressClassName: &className},
			Status: networkingv1.IngressStatus{LoadBalancer: networkingv1.IngressLoadBalancerStatus{
				Ingress: []networkingv1.IngressLoadBalancerIngress{{IP: "203.0.113.10"}},
			}},
		}
		for s := range spec.ServicesPerNamespace {
			app := fmt.Sprintf("svc-%02d", s)
			service := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: app, Labels: map[string]string{"app": app}},
				Spec: corev1.ServiceSpec{
					Type:     corev1.ServiceTypeClusterIP,
					Selector: map[string]string{"app": app},
					Ports:    []corev1.ServicePort{{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP, TargetPort: intstr.FromString("http")}},
				},
			}
			cluster.Services = append(cluster.Services, service)
			var pods []*corev1.Pod
			for replica := range spec.PodsPerService {
				pods = append(pods, nextPod(namespace, app, replica))
			}
			cluster.Pods = append(cluster.Pods, pods...)
			cluster.EndpointSlices = append(cluster.EndpointSlices, EndpointSlice(service, pods))

			pathType := networkingv1.PathTypePrefix
			ingress.Spec.Rules = append(ingress.Spec.Rules, networkingv1.IngressRule{
				Host: fmt.Sprintf("%s.%s.example.test", app, namespace),
				IngressRuleValue: networkingv1.IngressRuleValue{HTTP: &networkingv1.HTTPIngressRuleValue{
					Paths: []networkingv1.HTTPIngressPath{{
						Path:     "/",
						PathType: &pathType,
						Backend: networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{
							Name: app,
							Port: networkingv1.ServiceBackendPort{Name: "http"},
						}},
					}},
				}},
			})
		}
		cluster.Ingresses = append(cluster.Ingresses, ingress)
	}
	return cluster
}

// Objects returns every object, for a fake clientset.
func (c Cluster) Objects() []runtime.Object {
	var objects []runtime.Object
	for _, item := range c.IngressClasses {
		objects = append(objects, item)
	}
	for _, item := range c.Ingresses {
		objects = append(objects, item)
	}
	for _, item := range c.Services {
		objects = append(objects, item)
	}
	for _, item := range c.Pods {
		objects = append(objects, item)
	}
	for _, item := range c.Nodes {
		objects = append(objects, item)
	}
	for _, item := range c.EndpointSlices {
		objects = append(objects, item)
	}
	return objects
}

// Node returns a Ready Linux node with the fields a real kubelet reports,
// including the image list that dominates Node object size.
func Node(name string) *corev1.Node {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"kubernetes.io/hostname": name,
				"kubernetes.io/os":       "linux",
				"kubernetes.io/arch":     "amd64",
			},
			Annotations: map[string]string{"node.alpha.kubernetes.io/ttl": "0"},
		},
		Status: corev1.NodeStatus{
			NodeInfo: corev1.NodeSystemInfo{KubeletVersion: "v1.34.1", OSImage: "Ubuntu 24.04.3 LTS"},
			Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeMemoryPressure, Status: corev1.ConditionFalse, Reason: "KubeletHasSufficientMemory"},
				{Type: corev1.NodeDiskPressure, Status: corev1.ConditionFalse, Reason: "KubeletHasNoDiskPressure"},
				{Type: corev1.NodeReady, Status: corev1.ConditionTrue, Reason: "KubeletReady", Message: "kubelet is posting ready status"},
			},
			Addresses: []corev1.NodeAddress{{Type: corev1.NodeInternalIP, Address: "192.0.2.1"}, {Type: corev1.NodeHostName, Address: name}},
		},
	}
	for i := range 40 {
		node.Status.Images = append(node.Status.Images, corev1.ContainerImage{
			Names:     []string{fmt.Sprintf("registry.example.test/team/app-%02d@sha256:%064d", i, i), fmt.Sprintf("registry.example.test/team/app-%02d:v1.%d.0", i, i)},
			SizeBytes: int64(50_000_000 + i),
		})
	}
	return node
}

// Pod returns a Running, Ready Pod with one container.
func Pod(namespace, name string, labels map[string]string, nodeName, ip string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   namespace,
			Name:        name,
			Labels:      labels,
			Annotations: map[string]string{"kubectl.kubernetes.io/restartedAt": "2026-01-01T00:00:00Z"},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "apps/v1", Kind: "ReplicaSet", Name: name[:max(0, len(name)-6)], UID: "00000000-0000-0000-0000-000000000000",
			}},
		},
		Spec: corev1.PodSpec{
			NodeName: nodeName,
			Containers: []corev1.Container{{
				Name:  "app",
				Image: "registry.example.test/team/app:v1.0.0",
				Ports: []corev1.ContainerPort{{Name: "http", ContainerPort: 8080, Protocol: corev1.ProtocolTCP}},
				Env:   []corev1.EnvVar{{Name: "LOG_LEVEL", Value: "info"}, {Name: "PORT", Value: "8080"}},
			}},
		},
		Status: corev1.PodStatus{
			Phase:  corev1.PodRunning,
			PodIP:  ip,
			PodIPs: []corev1.PodIP{{IP: ip}},
			HostIP: "192.0.2.1",
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodInitialized, Status: corev1.ConditionTrue},
				{Type: corev1.PodReady, Status: corev1.ConditionTrue},
				{Type: corev1.ContainersReady, Status: corev1.ConditionTrue},
				{Type: corev1.PodScheduled, Status: corev1.ConditionTrue},
			},
			ContainerStatuses: []corev1.ContainerStatus{{
				Name:         "app",
				Ready:        true,
				Image:        "registry.example.test/team/app:v1.0.0",
				ImageID:      "registry.example.test/team/app@sha256:0000000000000000000000000000000000000000000000000000000000000000",
				ContainerID:  "containerd://" + name,
				State:        corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: metav1.Unix(1_700_000_000, 0)}},
				RestartCount: 0,
			}},
		},
	}
}

// EndpointSlice returns the slice the EndpointSlice controller would publish
// for service and its ready pods.
func EndpointSlice(service *corev1.Service, pods []*corev1.Pod) *discoveryv1.EndpointSlice {
	slice := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: service.Namespace,
			Name:      service.Name + "-x7k2p",
			Labels:    map[string]string{discoveryv1.LabelServiceName: service.Name, discoveryv1.LabelManagedBy: "endpointslice-controller.k8s.io"},
		},
		AddressType: discoveryv1.AddressTypeIPv4,
	}
	for _, port := range service.Spec.Ports {
		name := port.Name
		protocol := port.Protocol
		number := int32(8080)
		if port.TargetPort.Type == intstr.String && port.TargetPort.StrVal == "https" {
			number = 8443
		}
		slice.Ports = append(slice.Ports, discoveryv1.EndpointPort{Name: &name, Protocol: &protocol, Port: &number})
	}
	ready := true
	for _, pod := range pods {
		nodeName := pod.Spec.NodeName
		slice.Endpoints = append(slice.Endpoints, discoveryv1.Endpoint{
			Addresses:  []string{pod.Status.PodIP},
			Conditions: discoveryv1.EndpointConditions{Ready: &ready, Serving: &ready},
			NodeName:   &nodeName,
			TargetRef:  &corev1.ObjectReference{Kind: "Pod", Namespace: pod.Namespace, Name: pod.Name},
		})
	}
	return slice
}

func podIP(index int) string {
	return fmt.Sprintf("10.%d.%d.%d", 64+index/65536%64, index/256%256, index%256)
}
