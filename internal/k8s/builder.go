package k8s

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/emircanagac/northscope/internal/models"
)

func BuildTopology(
	ingresses []*networkingv1.Ingress,
	ingressClasses []*networkingv1.IngressClass,
	services []*corev1.Service,
	pods []*corev1.Pod,
	endpointSlices ...[]*discoveryv1.EndpointSlice,
) models.TopologySnapshot {
	return BuildTopologyWithResources(ingresses, ingressClasses, services, pods, nil, nil, endpointSlices...)
}

func BuildTopologyWithResources(
	ingresses []*networkingv1.Ingress,
	ingressClasses []*networkingv1.IngressClass,
	services []*corev1.Service,
	pods []*corev1.Pod,
	nodes []*corev1.Node,
	externalResources []ExternalResource,
	endpointSlices ...[]*discoveryv1.EndpointSlice,
) models.TopologySnapshot {
	builder := newTopologyBuilder()
	ingressClassesByName := make(map[string]*networkingv1.IngressClass, len(ingressClasses))
	servicesByKey := make(map[string]*corev1.Service, len(services))
	nodesByName := make(map[string]*corev1.Node, len(nodes))
	podsByNamespace := make(map[string][]*corev1.Pod)
	podsByName := make(map[string]*corev1.Pod, len(pods))
	podsByIP := make(map[string]*corev1.Pod, len(pods))
	servicePodsCache := make(map[string][]*corev1.Pod, len(services))
	endpointStatsByService := collectEndpointStats(flattenEndpointSlices(endpointSlices))

	for _, ingressClass := range ingressClasses {
		ingressClassesByName[ingressClass.Name] = ingressClass
		builder.addController(ingressClass.Name, ingressClass.Spec.Controller)
	}

	for _, node := range nodes {
		nodesByName[node.Name] = node
		builder.addKubernetesNode(node)
	}

	for _, pod := range pods {
		podsByNamespace[pod.Namespace] = append(podsByNamespace[pod.Namespace], pod)
		podsByName[namespacedKey(pod.Namespace, pod.Name)] = pod
		if pod.Status.PodIP != "" {
			podsByIP[namespacedKey(pod.Namespace, pod.Status.PodIP)] = pod
		}
		builder.addPod(pod)
		if pod.Spec.NodeName != "" {
			if _, ok := nodesByName[pod.Spec.NodeName]; ok {
				builder.addEdge(nodeID(models.NodeKindNode, "", pod.Spec.NodeName), nodeID(models.NodeKindPod, pod.Namespace, pod.Name), "hosts", "Node")
			}
		}
	}

	for _, svc := range services {
		servicesByKey[namespacedKey(svc.Namespace, svc.Name)] = svc
		builder.addService(svc)
		servicePodsCache[namespacedKey(svc.Namespace, svc.Name)] = matchingPodsForService(svc, podsByNamespace)
	}

	for _, svc := range services {
		servicePods := servicePodsCache[namespacedKey(svc.Namespace, svc.Name)]
		targetNodeID := nodeID(models.NodeKindService, svc.Namespace, svc.Name)
		if controllerClassName := matchingIngressClassName(svc, servicePods, ingressClassesByName); controllerClassName != "" {
			targetNodeID = controllerNodeID(controllerClassName)
		}

		switch svc.Spec.Type {
		case corev1.ServiceTypeLoadBalancer:
			builder.addLoadBalancer(svc)
			hasNodePort := false
			for _, port := range svc.Spec.Ports {
				if port.NodePort == 0 {
					continue
				}
				hasNodePort = true
				builder.addNodePort(svc, port)
				builder.addEdge(loadBalancerNodeID(svc.Namespace, svc.Name), nodePortNodeID(svc.Namespace, svc.Name, port), "exposes", "LoadBalancer")
				for _, node := range nodes {
					builder.addEdge(nodePortNodeID(svc.Namespace, svc.Name, port), nodeID(models.NodeKindNode, "", node.Name), "opens_on", "NodePort")
				}
				builder.addEdge(nodePortNodeID(svc.Namespace, svc.Name, port), targetNodeID, "forwards", "NodePort")
			}
			if !hasNodePort {
				builder.addEdge(loadBalancerNodeID(svc.Namespace, svc.Name), targetNodeID, "balances", "LoadBalancer")
			}
		case corev1.ServiceTypeNodePort:
			for _, port := range svc.Spec.Ports {
				if port.NodePort == 0 {
					continue
				}
				builder.addNodePort(svc, port)
				for _, node := range nodes {
					builder.addEdge(nodePortNodeID(svc.Namespace, svc.Name, port), nodeID(models.NodeKindNode, "", node.Name), "opens_on", "NodePort")
				}
				builder.addEdge(nodePortNodeID(svc.Namespace, svc.Name, port), targetNodeID, "forwards", "NodePort")
			}
		}
	}

	for _, ingress := range ingresses {
		builder.addIngress(ingress)
		for _, host := range ingressHosts(ingress) {
			builder.addDNS(host, nodeID(models.NodeKindIngress, ingress.Namespace, ingress.Name))
		}
		for _, address := range ingressLoadBalancerAddresses(ingress) {
			lbID := ingressLoadBalancerNodeID(ingress.Namespace, ingress.Name, address)
			builder.addExternalLoadBalancer(lbID, "Ingress LB", address, ingress.Namespace, ingress.Name, map[string]string{
				"resource": displayName(ingress.Namespace, ingress.Name),
				"provider": providerFromIngress(ingress),
			})
			builder.addEdge(lbID, nodeID(models.NodeKindIngress, ingress.Namespace, ingress.Name), "fronts", "Ingress status")
		}
		if controllerClassName := ingressControllerClassName(ingress, ingressClassesByName); controllerClassName != "" {
			builder.addEdge(
				controllerNodeID(controllerClassName),
				nodeID(models.NodeKindIngress, ingress.Namespace, ingress.Name),
				"controls",
				"IngressClass",
			)
		}
		controllerClassName := ingressControllerClassName(ingress, ingressClassesByName)
		for _, route := range ingressRoutes(ingress) {
			svc := servicesByKey[namespacedKey(ingress.Namespace, route.ServiceName)]
			pods := servicePodsCache[namespacedKey(ingress.Namespace, route.ServiceName)]
			diagnosis := diagnoseIngressRoute(ingress, route, controllerClassName, svc, pods, endpointStatsByService[namespacedKey(ingress.Namespace, route.ServiceName)])
			routeID := ingressRouteNodeID(ingress.Namespace, ingress.Name, route.ID)
			builder.addIngressRoute(ingress, route, diagnosis)
			builder.addEdge(
				nodeID(models.NodeKindIngress, ingress.Namespace, ingress.Name),
				routeID,
				"defines",
				route.EdgeLabel(),
			)
			if svc != nil {
				builder.addEdge(routeID, nodeID(models.NodeKindService, svc.Namespace, svc.Name), "routes", "HTTP backend")
			}
		}
	}

	for _, resource := range externalResources {
		builder.addExternalResource(resource, services)
	}

	for _, svc := range services {
		if len(svc.Spec.Selector) == 0 {
			continue
		}
		selector := labels.SelectorFromSet(svc.Spec.Selector)
		for _, pod := range podsByNamespace[svc.Namespace] {
			if selector.Matches(labels.Set(pod.Labels)) {
				builder.addEdge(
					nodeID(models.NodeKindService, svc.Namespace, svc.Name),
					nodeID(models.NodeKindPod, pod.Namespace, pod.Name),
					"selects",
					"Selector",
				)
			}
		}
	}

	for _, endpointSlice := range flattenEndpointSlices(endpointSlices) {
		svcName, ok := endpointSliceServiceName(endpointSlice)
		if !ok {
			continue
		}
		svc, ok := servicesByKey[namespacedKey(endpointSlice.Namespace, svcName)]
		if !ok || len(svc.Spec.Selector) > 0 {
			continue
		}
		for _, pod := range endpointSlicePods(endpointSlice, podsByName, podsByIP) {
			builder.addEdge(
				nodeID(models.NodeKindService, svc.Namespace, svc.Name),
				nodeID(models.NodeKindPod, pod.Namespace, pod.Name),
				"endpointslice",
				"EndpointSlice",
			)
		}
		for _, endpoint := range endpointSlice.Endpoints {
			for _, address := range endpoint.Addresses {
				if endpointReferencesPod(endpoint, endpointSlice.Namespace, address, podsByName, podsByIP) {
					continue
				}
				builder.addExternalEndpoint(endpointSlice, svcName, address, endpoint)
				builder.addEdge(
					nodeID(models.NodeKindService, svc.Namespace, svc.Name),
					endpointNodeID(endpointSlice.Namespace, svcName, endpointSlice.Name, address),
					"endpointslice",
					"EndpointSlice",
				)
			}
		}
	}

	return models.TopologySnapshot{
		GeneratedAt: time.Now().UTC(),
		Nodes:       builder.nodes(),
		Edges:       builder.edges(),
	}
}

func flattenEndpointSlices(endpointLists [][]*discoveryv1.EndpointSlice) []*discoveryv1.EndpointSlice {
	var endpointSlices []*discoveryv1.EndpointSlice
	for _, list := range endpointLists {
		endpointSlices = append(endpointSlices, list...)
	}
	return endpointSlices
}

type endpointStats struct {
	Total       int
	Usable      int
	Ready       int
	Serving     int
	Terminating int
	Slices      []string
}

func collectEndpointStats(endpointSlices []*discoveryv1.EndpointSlice) map[string]endpointStats {
	statsByService := map[string]endpointStats{}
	for _, endpointSlice := range endpointSlices {
		serviceName, ok := endpointSliceServiceName(endpointSlice)
		if !ok {
			continue
		}
		key := namespacedKey(endpointSlice.Namespace, serviceName)
		stats := statsByService[key]
		stats.Slices = append(stats.Slices, endpointSlice.Name)
		for _, endpoint := range endpointSlice.Endpoints {
			stats.Total++
			ready := conditionPtrValue(endpoint.Conditions.Ready, true)
			serving := conditionPtrValue(endpoint.Conditions.Serving, ready)
			terminating := conditionPtrValue(endpoint.Conditions.Terminating, false)
			if ready {
				stats.Ready++
			}
			if serving {
				stats.Serving++
			}
			if terminating {
				stats.Terminating++
			}
			if ready && serving && !terminating {
				stats.Usable++
			}
		}
		sort.Strings(stats.Slices)
		statsByService[key] = stats
	}
	return statsByService
}

func conditionPtrValue(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func endpointSlicePods(
	endpointSlice *discoveryv1.EndpointSlice,
	podsByName map[string]*corev1.Pod,
	podsByIP map[string]*corev1.Pod,
) []*corev1.Pod {
	seen := map[string]*corev1.Pod{}

	for _, endpoint := range endpointSlice.Endpoints {
		for _, address := range endpoint.Addresses {
			if endpoint.TargetRef != nil && endpoint.TargetRef.Kind == "Pod" && endpoint.TargetRef.Name != "" {
				namespace := endpoint.TargetRef.Namespace
				if namespace == "" {
					namespace = endpointSlice.Namespace
				}
				if pod, ok := podsByName[namespacedKey(namespace, endpoint.TargetRef.Name)]; ok {
					seen[namespacedKey(pod.Namespace, pod.Name)] = pod
					continue
				}
			}

			if pod, ok := podsByIP[namespacedKey(endpointSlice.Namespace, address)]; ok {
				seen[namespacedKey(pod.Namespace, pod.Name)] = pod
			}
		}
	}

	pods := make([]*corev1.Pod, 0, len(seen))
	for _, pod := range seen {
		pods = append(pods, pod)
	}
	sort.Slice(pods, func(i, j int) bool {
		return namespacedKey(pods[i].Namespace, pods[i].Name) < namespacedKey(pods[j].Namespace, pods[j].Name)
	})
	return pods
}

func endpointSliceServiceName(endpointSlice *discoveryv1.EndpointSlice) (string, bool) {
	serviceName := endpointSlice.Labels[discoveryv1.LabelServiceName]
	if serviceName == "" {
		return "", false
	}
	return serviceName, true
}

func endpointReferencesPod(
	endpoint discoveryv1.Endpoint,
	namespace string,
	address string,
	podsByName map[string]*corev1.Pod,
	podsByIP map[string]*corev1.Pod,
) bool {
	if endpoint.TargetRef != nil && endpoint.TargetRef.Kind == "Pod" && endpoint.TargetRef.Name != "" {
		targetNamespace := endpoint.TargetRef.Namespace
		if targetNamespace == "" {
			targetNamespace = namespace
		}
		if _, ok := podsByName[namespacedKey(targetNamespace, endpoint.TargetRef.Name)]; ok {
			return true
		}
	}
	_, ok := podsByIP[namespacedKey(namespace, address)]
	return ok
}

func ingressControllerClassName(ingress *networkingv1.Ingress, ingressClassesByName map[string]*networkingv1.IngressClass) string {
	className := ingressClassName(ingress)
	if className != "" {
		return className
	}
	if len(ingressClassesByName) == 1 {
		for name := range ingressClassesByName {
			return name
		}
	}
	return ""
}

func matchingIngressClassName(service *corev1.Service, pods []*corev1.Pod, ingressClassesByName map[string]*networkingv1.IngressClass) string {
	if len(ingressClassesByName) == 0 || !serviceLooksLikeIngressController(service, pods) {
		return ""
	}

	if len(ingressClassesByName) == 1 {
		for name := range ingressClassesByName {
			return name
		}
	}

	serviceText := strings.ToLower(strings.Join(serviceIdentityTerms(service, pods), " "))
	bestScore := 0
	bestName := ""
	for name, ingressClass := range ingressClassesByName {
		score := 0
		className := strings.ToLower(name)
		controller := strings.ToLower(ingressClass.Spec.Controller)
		if className != "" && strings.Contains(serviceText, className) {
			score += 2
		}
		if controller != "" && strings.Contains(serviceText, controller) {
			score += 2
		}
		if strings.Contains(serviceText, "ingress") {
			score++
		}
		if strings.Contains(serviceText, "controller") {
			score++
		}
		if score > bestScore {
			bestScore = score
			bestName = name
		}
	}

	if bestScore == 0 {
		return ""
	}
	return bestName
}

func serviceLooksLikeIngressController(service *corev1.Service, pods []*corev1.Pod) bool {
	haystack := strings.ToLower(strings.Join(serviceIdentityTerms(service, pods), " "))
	return strings.Contains(haystack, "ingress") || strings.Contains(haystack, "controller")
}

func serviceIdentityTerms(service *corev1.Service, pods []*corev1.Pod) []string {
	terms := []string{service.Namespace, service.Name, string(service.Spec.Type)}
	for key, value := range service.Labels {
		terms = append(terms, key, value)
	}
	for key, value := range service.Annotations {
		terms = append(terms, key, value)
	}
	for _, pod := range pods {
		terms = append(terms, pod.Namespace, pod.Name)
		for key, value := range pod.Labels {
			terms = append(terms, key, value)
		}
	}
	return terms
}

func matchingPodsForService(service *corev1.Service, podsByNamespace map[string][]*corev1.Pod) []*corev1.Pod {
	if len(service.Spec.Selector) == 0 {
		return nil
	}
	selector := labels.SelectorFromSet(service.Spec.Selector)
	var pods []*corev1.Pod
	for _, pod := range podsByNamespace[service.Namespace] {
		if selector.Matches(labels.Set(pod.Labels)) {
			pods = append(pods, pod)
		}
	}
	return pods
}

func loadBalancerAddresses(service *corev1.Service) []string {
	addresses := make([]string, 0, len(service.Status.LoadBalancer.Ingress)+len(service.Spec.ExternalIPs))
	for _, ingress := range service.Status.LoadBalancer.Ingress {
		if ingress.IP != "" {
			addresses = append(addresses, ingress.IP)
		}
		if ingress.Hostname != "" {
			addresses = append(addresses, ingress.Hostname)
		}
	}
	addresses = append(addresses, service.Spec.ExternalIPs...)
	return addresses
}

func nodePortName(service *corev1.Service, port corev1.ServicePort) string {
	if port.Name != "" {
		return service.Name + ":" + port.Name
	}
	return fmt.Sprintf("%s:%d", service.Name, port.Port)
}

func nodePortLabel(service *corev1.Service, port corev1.ServicePort) string {
	label := nodePortName(service, port)
	if port.NodePort != 0 {
		label += " -> " + strconv.Itoa(int(port.NodePort))
	}
	return label
}

func controllerNodeID(className string) string {
	return nodeID(models.NodeKindController, "", className)
}

func loadBalancerNodeID(namespace, name string) string {
	return nodeID(models.NodeKindLoadBalancer, namespace, name)
}

func nodePortNodeID(namespace, serviceName string, port corev1.ServicePort) string {
	return nodeID(models.NodeKindNodePort, namespace, serviceName+":"+nodePortNameForID(port))
}

func nodePortNameForID(port corev1.ServicePort) string {
	if port.Name != "" {
		return port.Name
	}
	return strconv.Itoa(int(port.Port))
}

type ingressRoute struct {
	ID          string
	Host        string
	Path        string
	PathType    string
	ServiceName string
	ServicePort networkingv1.ServiceBackendPort
	IsDefault   bool
}

func (r ingressRoute) Name() string {
	host := r.Host
	if host == "" {
		host = "*"
	}
	path := r.Path
	if path == "" {
		path = "/"
	}
	if r.IsDefault {
		return "default -> " + r.ServiceName
	}
	return host + " " + path
}

func (r ingressRoute) EdgeLabel() string {
	if r.IsDefault {
		return "default"
	}
	return r.Name()
}

func (r ingressRoute) ServicePortLabel() string {
	if r.ServicePort.Name != "" {
		return r.ServicePort.Name
	}
	if r.ServicePort.Number != 0 {
		return strconv.Itoa(int(r.ServicePort.Number))
	}
	return "unspecified"
}

type routeDiagnosis struct {
	Status     string
	Severity   string
	Message    string
	NextStep   string
	Kubectl    string
	Confidence string
}

func diagnoseIngressRoute(
	ingress *networkingv1.Ingress,
	route ingressRoute,
	controllerClassName string,
	service *corev1.Service,
	servicePods []*corev1.Pod,
	stats endpointStats,
) routeDiagnosis {
	baseKubectl := fmt.Sprintf("kubectl describe ingress %s -n %s", ingress.Name, ingress.Namespace)
	if service == nil {
		return routeDiagnosis{
			Status:     "Error",
			Severity:   "error",
			Message:    fmt.Sprintf("Backend Service %q does not exist in namespace %q.", route.ServiceName, ingress.Namespace),
			NextStep:   "Create the Service or fix the backend.service.name in the Ingress rule.",
			Kubectl:    baseKubectl,
			Confidence: "Certain",
		}
	}
	if !serviceHasBackendPort(service, route.ServicePort) {
		return routeDiagnosis{
			Status:     "Error",
			Severity:   "error",
			Message:    fmt.Sprintf("Service %q has no port matching backend port %q.", service.Name, route.ServicePortLabel()),
			NextStep:   "Check spec.ports[].name/port on the Service and the Ingress backend port.",
			Kubectl:    fmt.Sprintf("kubectl get svc %s -n %s -o yaml", service.Name, service.Namespace),
			Confidence: "Certain",
		}
	}
	if controllerClassName == "" {
		return routeDiagnosis{
			Status:     "Warning",
			Severity:   "warning",
			Message:    "No matching IngressClass/controller was found for this Ingress.",
			NextStep:   "Check ingressClassName, the default IngressClass, and the controller deployment.",
			Kubectl:    baseKubectl,
			Confidence: "Inferred",
		}
	}
	if len(service.Spec.Selector) > 0 && len(servicePods) == 0 {
		return routeDiagnosis{
			Status:     "Error",
			Severity:   "error",
			Message:    fmt.Sprintf("Service %q selector matches 0 Pods.", service.Name),
			NextStep:   "Compare Service selector labels with Pod labels in this namespace.",
			Kubectl:    fmt.Sprintf("kubectl get svc %s -n %s -o yaml; kubectl get pods -n %s --show-labels", service.Name, service.Namespace, service.Namespace),
			Confidence: "Certain",
		}
	}
	readyPods := countReadyPods(servicePods)
	if len(servicePods) > 0 && readyPods == 0 {
		return routeDiagnosis{
			Status:     "Error",
			Severity:   "error",
			Message:    fmt.Sprintf("Service %q selects Pods, but none are Ready.", service.Name),
			NextStep:   "Inspect Pod readiness, container status, probes, and recent events.",
			Kubectl:    fmt.Sprintf("kubectl get pods -n %s -l %s; kubectl describe pod -n %s -l %s", service.Namespace, labelSelectorForDisplay(service.Spec.Selector), service.Namespace, labelSelectorForDisplay(service.Spec.Selector)),
			Confidence: "Certain",
		}
	}
	if stats.Total > 0 && stats.Usable == 0 {
		return routeDiagnosis{
			Status:     "Error",
			Severity:   "error",
			Message:    fmt.Sprintf("Service %q has EndpointSlices, but 0 usable endpoints.", service.Name),
			NextStep:   "Check EndpointSlice ready/serving/terminating conditions and Pod readiness.",
			Kubectl:    fmt.Sprintf("kubectl get endpointslice -n %s -l kubernetes.io/service-name=%s -o wide", service.Namespace, service.Name),
			Confidence: "Certain",
		}
	}
	if stats.Total == 0 && len(service.Spec.Selector) == 0 {
		return routeDiagnosis{
			Status:     "Warning",
			Severity:   "warning",
			Message:    fmt.Sprintf("Selector-less Service %q has no EndpointSlice data.", service.Name),
			NextStep:   "Verify manually managed EndpointSlices or external backend wiring.",
			Kubectl:    fmt.Sprintf("kubectl get endpointslice -n %s -l kubernetes.io/service-name=%s -o yaml", service.Namespace, service.Name),
			Confidence: "Certain",
		}
	}
	if stats.Total == 0 && len(service.Spec.Selector) > 0 {
		return routeDiagnosis{
			Status:     "Warning",
			Severity:   "warning",
			Message:    fmt.Sprintf("Service %q has Ready Pods, but no EndpointSlice was observed.", service.Name),
			NextStep:   "Check EndpointSlice RBAC and the endpoint slice controller if traffic still fails.",
			Kubectl:    fmt.Sprintf("kubectl get endpointslice -n %s -l kubernetes.io/service-name=%s", service.Namespace, service.Name),
			Confidence: "Inferred",
		}
	}
	if stats.Terminating > 0 {
		return routeDiagnosis{
			Status:     "Warning",
			Severity:   "warning",
			Message:    fmt.Sprintf("Service %q has %d terminating endpoint(s).", service.Name, stats.Terminating),
			NextStep:   "Check rollout status and whether all remaining endpoints are serving.",
			Kubectl:    fmt.Sprintf("kubectl rollout status deployment -n %s; kubectl get endpointslice -n %s -l kubernetes.io/service-name=%s -o wide", service.Namespace, service.Namespace, service.Name),
			Confidence: "Inferred",
		}
	}
	return routeDiagnosis{
		Status:     "Healthy",
		Severity:   "ok",
		Message:    fmt.Sprintf("Route resolves to Service %q with %d usable endpoint(s).", service.Name, maxInt(stats.Usable, readyPods)),
		NextStep:   "If users still fail, verify controller logs, cloud/F5 load balancer health checks, TLS, and NetworkPolicy.",
		Kubectl:    fmt.Sprintf("kubectl describe ingress %s -n %s; kubectl get endpointslice -n %s -l kubernetes.io/service-name=%s -o wide", ingress.Name, ingress.Namespace, service.Namespace, service.Name),
		Confidence: "Configured",
	}
}

func serviceHasBackendPort(service *corev1.Service, backendPort networkingv1.ServiceBackendPort) bool {
	for _, port := range service.Spec.Ports {
		if backendPort.Name != "" && port.Name == backendPort.Name {
			return true
		}
		if backendPort.Number != 0 && port.Port == backendPort.Number {
			return true
		}
	}
	return false
}

func countReadyPods(pods []*corev1.Pod) int {
	ready := 0
	for _, pod := range pods {
		if podIsReady(pod) {
			ready++
		}
	}
	return ready
}

func podIsReady(pod *corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return pod.Status.Phase == corev1.PodRunning
}

func labelSelectorForDisplay(selector map[string]string) string {
	if len(selector) == 0 {
		return ""
	}
	parts := make([]string, 0, len(selector))
	for key, value := range selector {
		parts = append(parts, key+"="+value)
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

type topologyBuilder struct {
	nodesByID  map[string]models.Node
	edgesByID  map[string]models.Edge
	nextColumn map[models.NodeKind]int
}

func newTopologyBuilder() *topologyBuilder {
	return &topologyBuilder{
		nodesByID:  make(map[string]models.Node),
		edgesByID:  make(map[string]models.Edge),
		nextColumn: make(map[models.NodeKind]int),
	}
}

func (b *topologyBuilder) addIngress(ingress *networkingv1.Ingress) {
	properties := map[string]string{}
	if className := ingressClassName(ingress); className != "" {
		properties["className"] = className
	}
	if hosts := ingressHosts(ingress); len(hosts) > 0 {
		properties["hosts"] = strings.Join(hosts, ", ")
	}

	b.addNode(models.Node{
		ID:       nodeID(models.NodeKindIngress, ingress.Namespace, ingress.Name),
		Type:     "northscopeNode",
		Position: b.nextPosition(models.NodeKindIngress),
		Data: models.NodeData{
			Label:      displayName(ingress.Namespace, ingress.Name),
			Kind:       models.NodeKindIngress,
			Namespace:  ingress.Namespace,
			Name:       ingress.Name,
			Status:     ingressStatus(ingress),
			Metadata:   ingress.Labels,
			Properties: properties,
		},
	})
}

func (b *topologyBuilder) addIngressRoute(ingress *networkingv1.Ingress, route ingressRoute, diagnosis routeDiagnosis) {
	properties := map[string]string{
		"mode":        "configured",
		"ingress":     displayName(ingress.Namespace, ingress.Name),
		"backend":     route.ServiceName + ":" + route.ServicePortLabel(),
		"service":     route.ServiceName,
		"servicePort": route.ServicePortLabel(),
		"diagnosis":   diagnosis.Message,
		"nextStep":    diagnosis.NextStep,
		"kubectl":     diagnosis.Kubectl,
		"confidence":  diagnosis.Confidence,
		"severity":    diagnosis.Severity,
	}
	if route.Host != "" {
		properties["host"] = route.Host
	}
	if route.Path != "" {
		properties["path"] = route.Path
	}
	if route.PathType != "" {
		properties["pathType"] = route.PathType
	}
	if route.IsDefault {
		properties["defaultBackend"] = "true"
	}

	b.addNode(models.Node{
		ID:       ingressRouteNodeID(ingress.Namespace, ingress.Name, route.ID),
		Type:     "northscopeNode",
		Position: b.nextPosition(models.NodeKindRoute),
		Data: models.NodeData{
			Label:      route.Name(),
			Kind:       models.NodeKindRoute,
			Namespace:  ingress.Namespace,
			Name:       route.Name(),
			Status:     diagnosis.Status,
			Properties: properties,
		},
	})
}

func (b *topologyBuilder) addController(className, controller string) {
	properties := map[string]string{}
	if className != "" {
		properties["className"] = className
	}
	if controller != "" {
		properties["controller"] = controller
	}

	b.addNode(models.Node{
		ID:       controllerNodeID(className),
		Type:     "northscopeNode",
		Position: b.nextPosition(models.NodeKindController),
		Data: models.NodeData{
			Label:      className,
			Kind:       models.NodeKindController,
			Name:       className,
			Status:     controller,
			Properties: properties,
		},
	})
}

func (b *topologyBuilder) addService(service *corev1.Service) {
	properties := map[string]string{
		"type": string(service.Spec.Type),
	}

	if service.Spec.ClusterIP != "" && service.Spec.ClusterIP != corev1.ClusterIPNone {
		properties["clusterIP"] = service.Spec.ClusterIP
	}
	if len(service.Spec.Ports) > 0 {
		properties["ports"] = servicePorts(service)
	}

	b.addNode(models.Node{
		ID:       nodeID(models.NodeKindService, service.Namespace, service.Name),
		Type:     "northscopeNode",
		Position: b.nextPosition(models.NodeKindService),
		Data: models.NodeData{
			Label:      displayName(service.Namespace, service.Name),
			Kind:       models.NodeKindService,
			Namespace:  service.Namespace,
			Name:       service.Name,
			Status:     serviceStatus(service),
			Metadata:   service.Labels,
			Properties: properties,
		},
	})
}

func (b *topologyBuilder) addLoadBalancer(service *corev1.Service) {
	properties := map[string]string{
		"serviceType": string(service.Spec.Type),
	}
	if addresses := loadBalancerAddresses(service); len(addresses) > 0 {
		properties["addresses"] = strings.Join(addresses, ", ")
	}
	if len(service.Spec.ExternalIPs) > 0 {
		properties["externalIPs"] = strings.Join(service.Spec.ExternalIPs, ", ")
	}

	b.addNode(models.Node{
		ID:       loadBalancerNodeID(service.Namespace, service.Name),
		Type:     "northscopeNode",
		Position: b.nextPosition(models.NodeKindLoadBalancer),
		Data: models.NodeData{
			Label:      displayName(service.Namespace, service.Name),
			Kind:       models.NodeKindLoadBalancer,
			Namespace:  service.Namespace,
			Name:       service.Name,
			Status:     serviceStatus(service),
			Properties: properties,
		},
	})
}

func (b *topologyBuilder) addNodePort(service *corev1.Service, port corev1.ServicePort) {
	properties := map[string]string{
		"service":     displayName(service.Namespace, service.Name),
		"servicePort": strconv.Itoa(int(port.Port)),
	}
	if port.NodePort != 0 {
		properties["nodePort"] = strconv.Itoa(int(port.NodePort))
	}
	if port.Protocol != "" {
		properties["protocol"] = string(port.Protocol)
	}

	b.addNode(models.Node{
		ID:       nodePortNodeID(service.Namespace, service.Name, port),
		Type:     "northscopeNode",
		Position: b.nextPosition(models.NodeKindNodePort),
		Data: models.NodeData{
			Label:      nodePortLabel(service, port),
			Kind:       models.NodeKindNodePort,
			Namespace:  service.Namespace,
			Name:       nodePortName(service, port),
			Status:     string(service.Spec.Type),
			Properties: properties,
		},
	})
}

func (b *topologyBuilder) addKubernetesNode(node *corev1.Node) {
	properties := map[string]string{}
	if node.Status.NodeInfo.KubeletVersion != "" {
		properties["kubelet"] = node.Status.NodeInfo.KubeletVersion
	}
	if node.Status.NodeInfo.OSImage != "" {
		properties["os"] = node.Status.NodeInfo.OSImage
	}

	status := "Unknown"
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeReady {
			status = string(condition.Status)
			break
		}
	}

	b.addNode(models.Node{
		ID:       nodeID(models.NodeKindNode, "", node.Name),
		Type:     "northscopeNode",
		Position: b.nextPosition(models.NodeKindNode),
		Data: models.NodeData{
			Label:      node.Name,
			Kind:       models.NodeKindNode,
			Name:       node.Name,
			Status:     "Ready=" + status,
			Metadata:   node.Labels,
			Properties: properties,
		},
	})
}

func (b *topologyBuilder) addPod(pod *corev1.Pod) {
	properties := map[string]string{}
	if pod.Spec.NodeName != "" {
		properties["nodeName"] = pod.Spec.NodeName
	}
	if pod.Status.PodIP != "" {
		properties["podIP"] = pod.Status.PodIP
	}

	b.addNode(models.Node{
		ID:       nodeID(models.NodeKindPod, pod.Namespace, pod.Name),
		Type:     "northscopeNode",
		Position: b.nextPosition(models.NodeKindPod),
		Data: models.NodeData{
			Label:      displayName(pod.Namespace, pod.Name),
			Kind:       models.NodeKindPod,
			Namespace:  pod.Namespace,
			Name:       pod.Name,
			Status:     podStatus(pod),
			Phase:      string(pod.Status.Phase),
			Metadata:   pod.Labels,
			Properties: properties,
		},
	})
}

func (b *topologyBuilder) addExternalEndpoint(endpointSlice *discoveryv1.EndpointSlice, serviceName, address string, endpoint discoveryv1.Endpoint) {
	if address == "" {
		return
	}

	ready := conditionPtrValue(endpoint.Conditions.Ready, true)
	serving := conditionPtrValue(endpoint.Conditions.Serving, ready)
	terminating := conditionPtrValue(endpoint.Conditions.Terminating, false)
	status := "Ready"
	if !ready || !serving || terminating {
		status = "NotReady"
	}

	properties := map[string]string{
		"address":      address,
		"endpointType": "external",
		"service":      displayName(endpointSlice.Namespace, serviceName),
		"slice":        endpointSlice.Name,
	}
	if len(endpointSlice.Ports) > 0 {
		properties["ports"] = endpointSlicePorts(endpointSlice)
	}
	if endpoint.TargetRef != nil {
		properties["targetRef"] = strings.Trim(endpoint.TargetRef.Kind+"/"+endpoint.TargetRef.Name, "/")
	}

	b.addNode(models.Node{
		ID:       endpointNodeID(endpointSlice.Namespace, serviceName, endpointSlice.Name, address),
		Type:     "northscopeNode",
		Position: b.nextPosition(models.NodeKindEndpointSlice),
		Data: models.NodeData{
			Label:      address,
			Kind:       models.NodeKindEndpointSlice,
			Namespace:  endpointSlice.Namespace,
			Name:       address,
			Status:     status,
			Properties: properties,
		},
	})
}

func (b *topologyBuilder) addExternalResource(resource ExternalResource, services []*corev1.Service) {
	switch resource.Kind {
	case ExternalKindGatewayClass:
		controller := resource.Properties["controllerName"]
		if controller == "" {
			controller = "GatewayClass"
		}
		b.addController(resource.Name, controller)
	case ExternalKindGateway:
		b.addGateway(resource)
		if resource.ClassName != "" {
			b.addEdge(controllerNodeID(resource.ClassName), gatewayNodeID(resource.Namespace, resource.Name), "controls", "GatewayClass")
		}
		for _, address := range resource.Addresses {
			lbID := gatewayLoadBalancerNodeID(resource.Namespace, resource.Name, address)
			b.addExternalLoadBalancer(lbID, "Gateway LB", address, resource.Namespace, resource.Name, map[string]string{
				"resource": displayName(resource.Namespace, resource.Name),
				"provider": "Gateway API",
			})
			b.addEdge(lbID, gatewayNodeID(resource.Namespace, resource.Name), "fronts", "Gateway status")
		}
		for _, hostname := range resource.Hostnames {
			b.addDNS(hostname, gatewayNodeID(resource.Namespace, resource.Name))
		}
	case ExternalKindHTTPRoute, ExternalKindGRPCRoute, ExternalKindTLSRoute, ExternalKindTCPRoute, ExternalKindUDPRoute:
		b.addRoute(resource)
		for _, parent := range resource.ParentRefs {
			if strings.EqualFold(parent.Kind, "Gateway") || parent.Kind == "" {
				namespace := parent.Namespace
				if namespace == "" {
					namespace = resource.Namespace
				}
				b.addEdge(gatewayNodeID(namespace, parent.Name), routeNodeID(resource.Namespace, resource.Name, string(resource.Kind)), "attaches", string(resource.Kind))
			}
		}
		for _, backend := range resource.Backends {
			if backend.Kind != "" && !strings.EqualFold(backend.Kind, "Service") {
				continue
			}
			namespace := backend.Namespace
			if namespace == "" {
				namespace = resource.Namespace
			}
			b.addEdge(routeNodeID(resource.Namespace, resource.Name, string(resource.Kind)), nodeID(models.NodeKindService, namespace, backend.Name), "routes", string(resource.Kind))
		}
		for _, hostname := range resource.Hostnames {
			b.addDNS(hostname, routeNodeID(resource.Namespace, resource.Name, string(resource.Kind)))
		}
	case ExternalKindF5IngressLink, ExternalKindF5Virtual, ExternalKindF5Transport:
		lbID := externalResourceNodeID(resource)
		address := firstNonEmpty(resource.Addresses)
		b.addExternalLoadBalancer(lbID, string(resource.Kind), address, resource.Namespace, resource.Name, externalProperties(resource, "F5"))
		for _, hostname := range resource.Hostnames {
			b.addDNS(hostname, lbID)
		}
		for _, backend := range resource.Backends {
			if backend.Kind != "" && !strings.EqualFold(backend.Kind, "Service") {
				continue
			}
			namespace := backend.Namespace
			if namespace == "" {
				namespace = resource.Namespace
			}
			b.addEdge(lbID, nodeID(models.NodeKindService, namespace, backend.Name), "balances", string(resource.Kind))
		}
		for _, service := range servicesMatchingSelector(services, resource.Namespace, resource.Selector) {
			b.addEdge(lbID, nodeID(models.NodeKindService, service.Namespace, service.Name), "balances", string(resource.Kind))
		}
	}
}

func (b *topologyBuilder) addGateway(resource ExternalResource) {
	properties := externalProperties(resource, "Gateway API")
	if resource.ClassName != "" {
		properties["className"] = resource.ClassName
	}
	if len(resource.Listeners) > 0 {
		properties["listeners"] = strings.Join(resource.Listeners, ", ")
	}
	if len(resource.Addresses) > 0 {
		properties["addresses"] = strings.Join(resource.Addresses, ", ")
	}

	b.addNode(models.Node{
		ID:       gatewayNodeID(resource.Namespace, resource.Name),
		Type:     "northscopeNode",
		Position: b.nextPosition(models.NodeKindGateway),
		Data: models.NodeData{
			Label:      displayName(resource.Namespace, resource.Name),
			Kind:       models.NodeKindGateway,
			Namespace:  resource.Namespace,
			Name:       resource.Name,
			Status:     resourceStatus(resource),
			Properties: properties,
		},
	})
}

func (b *topologyBuilder) addRoute(resource ExternalResource) {
	properties := externalProperties(resource, "Gateway API")
	if len(resource.Hostnames) > 0 {
		properties["hostnames"] = strings.Join(resource.Hostnames, ", ")
	}

	b.addNode(models.Node{
		ID:       routeNodeID(resource.Namespace, resource.Name, string(resource.Kind)),
		Type:     "northscopeNode",
		Position: b.nextPosition(models.NodeKindRoute),
		Data: models.NodeData{
			Label:      displayName(resource.Namespace, resource.Name),
			Kind:       models.NodeKindRoute,
			Namespace:  resource.Namespace,
			Name:       resource.Name,
			Status:     resourceStatus(resource),
			Properties: properties,
		},
	})
}

func (b *topologyBuilder) addDNS(hostname, targetID string) {
	if hostname == "" {
		return
	}
	dnsID := nodeID(models.NodeKindDNS, "", hostname)
	b.addNode(models.Node{
		ID:       dnsID,
		Type:     "northscopeNode",
		Position: b.nextPosition(models.NodeKindDNS),
		Data: models.NodeData{
			Label: hostname,
			Kind:  models.NodeKindDNS,
			Name:  hostname,
		},
	})
	b.addEdge(dnsID, targetID, "resolves", "DNS")
}

func (b *topologyBuilder) addExternalLoadBalancer(id, label, address, namespace, name string, properties map[string]string) {
	if properties == nil {
		properties = map[string]string{}
	}
	if address != "" {
		properties["address"] = address
	}

	b.addNode(models.Node{
		ID:       id,
		Type:     "northscopeNode",
		Position: b.nextPosition(models.NodeKindLoadBalancer),
		Data: models.NodeData{
			Label:      labelOrDefault(label, displayName(namespace, name)),
			Kind:       models.NodeKindLoadBalancer,
			Namespace:  namespace,
			Name:       name,
			Status:     loadBalancerStatus(address),
			Properties: properties,
		},
	})
}

func (b *topologyBuilder) addNode(node models.Node) {
	b.nodesByID[node.ID] = node
}

func (b *topologyBuilder) addEdge(source, target, kind, label string) {
	id := edgeID(source, target, kind)
	b.edgesByID[id] = models.Edge{
		ID:       id,
		Source:   source,
		Target:   target,
		Type:     "smoothstep",
		Label:    label,
		Animated: kind == "routes",
		Data: &models.EdgeData{
			Kind: kind,
		},
	}
}

func (b *topologyBuilder) nodes() []models.Node {
	nodes := make([]models.Node, 0, len(b.nodesByID))
	for _, node := range b.nodesByID {
		nodes = append(nodes, node)
	}
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].ID < nodes[j].ID
	})
	return nodes
}

func (b *topologyBuilder) edges() []models.Edge {
	edges := make([]models.Edge, 0, len(b.edgesByID))
	for _, edge := range b.edgesByID {
		edges = append(edges, edge)
	}
	sort.Slice(edges, func(i, j int) bool {
		return edges[i].ID < edges[j].ID
	})
	return edges
}

func (b *topologyBuilder) nextPosition(kind models.NodeKind) models.Position {
	row := b.nextColumn[kind]
	b.nextColumn[kind]++

	return models.Position{
		X: kindColumn(kind),
		Y: float64(row * 140),
	}
}

func ingressRoutes(ingress *networkingv1.Ingress) []ingressRoute {
	var routes []ingressRoute
	add := func(host, path, pathType string, backend networkingv1.IngressBackend, isDefault bool) {
		if backend.Service == nil || backend.Service.Name == "" {
			return
		}
		route := ingressRoute{
			Host:        host,
			Path:        path,
			PathType:    pathType,
			ServiceName: backend.Service.Name,
			ServicePort: backend.Service.Port,
			IsDefault:   isDefault,
		}
		route.ID = safeID(strings.Join([]string{
			boolRoutePart(isDefault),
			host,
			path,
			pathType,
			backend.Service.Name,
			route.ServicePortLabel(),
			strconv.Itoa(len(routes)),
		}, "|"))
		routes = append(routes, route)
	}

	if ingress.Spec.DefaultBackend != nil {
		add("", "", "", *ingress.Spec.DefaultBackend, true)
	}
	for _, rule := range ingress.Spec.Rules {
		if rule.HTTP == nil {
			continue
		}
		for _, path := range rule.HTTP.Paths {
			add(rule.Host, path.Path, ingressPathType(path.PathType), path.Backend, false)
		}
	}
	return routes
}

func ingressPathType(pathType *networkingv1.PathType) string {
	if pathType == nil {
		return ""
	}
	return string(*pathType)
}

func boolRoutePart(value bool) string {
	if value {
		return "default"
	}
	return "rule"
}

func ingressClassName(ingress *networkingv1.Ingress) string {
	if ingress.Spec.IngressClassName != nil {
		return *ingress.Spec.IngressClassName
	}
	return ingress.Annotations["kubernetes.io/ingress.class"]
}

func ingressStatus(_ *networkingv1.Ingress) string {
	return "Configured"
}

func serviceStatus(service *corev1.Service) string {
	if service.Spec.Type == corev1.ServiceTypeLoadBalancer && len(service.Status.LoadBalancer.Ingress) == 0 {
		return "Pending"
	}
	return "Active"
}

func podStatus(pod *corev1.Pod) string {
	for _, status := range pod.Status.ContainerStatuses {
		if status.State.Waiting != nil && status.State.Waiting.Reason != "" {
			return status.State.Waiting.Reason
		}
		if status.State.Terminated != nil && status.State.Terminated.Reason != "" {
			return status.State.Terminated.Reason
		}
	}
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady && condition.Status != corev1.ConditionTrue {
			if condition.Reason != "" {
				return condition.Reason
			}
			return "NotReady"
		}
	}
	if pod.Status.Phase != "" {
		return string(pod.Status.Phase)
	}
	return "Unknown"
}

func ingressHosts(ingress *networkingv1.Ingress) []string {
	hosts := make([]string, 0, len(ingress.Spec.Rules))
	for _, rule := range ingress.Spec.Rules {
		if rule.Host != "" {
			hosts = append(hosts, rule.Host)
		}
	}
	sort.Strings(hosts)
	return hosts
}

func ingressLoadBalancerAddresses(ingress *networkingv1.Ingress) []string {
	addresses := make([]string, 0, len(ingress.Status.LoadBalancer.Ingress))
	for _, item := range ingress.Status.LoadBalancer.Ingress {
		if item.IP != "" {
			addresses = append(addresses, item.IP)
		}
		if item.Hostname != "" {
			addresses = append(addresses, item.Hostname)
		}
	}
	sort.Strings(addresses)
	return addresses
}

func servicePorts(service *corev1.Service) string {
	ports := make([]string, 0, len(service.Spec.Ports))
	for _, port := range service.Spec.Ports {
		value := strconv.Itoa(int(port.Port))
		if port.NodePort != 0 {
			value += ":" + strconv.Itoa(int(port.NodePort))
		}
		if port.Protocol != "" {
			value += "/" + string(port.Protocol)
		}
		ports = append(ports, value)
	}
	return strings.Join(ports, ", ")
}

func endpointSlicePorts(endpointSlice *discoveryv1.EndpointSlice) string {
	ports := make([]string, 0, len(endpointSlice.Ports))
	for _, port := range endpointSlice.Ports {
		if port.Port == nil {
			continue
		}
		name := ""
		if port.Name != nil {
			name = *port.Name
		}
		protocol := string(corev1.ProtocolTCP)
		if port.Protocol != nil {
			protocol = string(*port.Protocol)
		}
		value := strconv.Itoa(int(*port.Port)) + "/" + protocol
		if name != "" {
			value = name + ":" + value
		}
		ports = append(ports, value)
	}
	return strings.Join(ports, ", ")
}

func kindColumn(kind models.NodeKind) float64 {
	switch kind {
	case models.NodeKindDNS:
		return 0
	case models.NodeKindLoadBalancer:
		return 80
	case models.NodeKindNodePort:
		return 250
	case models.NodeKindNode:
		return 340
	case models.NodeKindController:
		return 420
	case models.NodeKindGateway:
		return 520
	case models.NodeKindIngress:
		return 620
	case models.NodeKindRoute:
		return 760
	case models.NodeKindService:
		return 940
	case models.NodeKindEndpointSlice:
		return 1110
	case models.NodeKindPod:
		return 1280
	default:
		return 0
	}
}

func namespacedKey(namespace, name string) string {
	return namespace + "/" + name
}

func displayName(namespace, name string) string {
	if namespace == "" {
		return name
	}
	return namespace + "/" + name
}

func nodeID(kind models.NodeKind, namespace, name string) string {
	return strings.ToLower(string(kind)) + ":" + namespace + ":" + name
}

func gatewayNodeID(namespace, name string) string {
	return nodeID(models.NodeKindGateway, namespace, name)
}

func routeNodeID(namespace, name, kind string) string {
	return nodeID(models.NodeKindRoute, namespace, kind+":"+name)
}

func ingressRouteNodeID(namespace, ingressName, routeID string) string {
	return nodeID(models.NodeKindRoute, namespace, "ingress:"+ingressName+":"+routeID)
}

func ingressLoadBalancerNodeID(namespace, name, address string) string {
	return nodeID(models.NodeKindLoadBalancer, namespace, "ingress:"+name+":"+safeID(address))
}

func endpointNodeID(namespace, serviceName, sliceName, address string) string {
	return nodeID(models.NodeKindEndpointSlice, namespace, serviceName+":"+sliceName+":"+safeID(address))
}

func gatewayLoadBalancerNodeID(namespace, name, address string) string {
	return nodeID(models.NodeKindLoadBalancer, namespace, "gateway:"+name+":"+safeID(address))
}

func externalResourceNodeID(resource ExternalResource) string {
	return nodeID(models.NodeKindLoadBalancer, resource.Namespace, strings.ToLower(string(resource.Kind))+":"+resource.Name)
}

func edgeID(source, target, kind string) string {
	return source + "->" + target + ":" + kind
}

func externalProperties(resource ExternalResource, provider string) map[string]string {
	properties := map[string]string{
		"provider": provider,
		"kind":     string(resource.Kind),
	}
	if resource.APIVersion != "" {
		properties["apiVersion"] = resource.APIVersion
	}
	for key, value := range resource.Properties {
		properties[key] = value
	}
	if len(resource.Addresses) > 0 {
		properties["addresses"] = strings.Join(resource.Addresses, ", ")
	}
	return properties
}

func resourceStatus(resource ExternalResource) string {
	if status := resource.Properties["status"]; status != "" {
		return status
	}
	if len(resource.Addresses) > 0 {
		return "Active"
	}
	return string(resource.Kind)
}

func loadBalancerStatus(address string) string {
	if address == "" {
		return "Pending"
	}
	return "Active"
}

func providerFromIngress(ingress *networkingv1.Ingress) string {
	if value := ingress.Annotations["kubernetes.io/ingress.class"]; value != "" {
		return value
	}
	if ingress.Spec.IngressClassName != nil {
		return *ingress.Spec.IngressClassName
	}
	return "Ingress"
}

func servicesMatchingSelector(services []*corev1.Service, namespace string, selector map[string]string) []*corev1.Service {
	if len(selector) == 0 {
		return nil
	}
	var matches []*corev1.Service
	for _, service := range services {
		if namespace != "" && service.Namespace != namespace {
			continue
		}
		if selectorMatches(service.Labels, selector) {
			matches = append(matches, service)
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		return namespacedKey(matches[i].Namespace, matches[i].Name) < namespacedKey(matches[j].Namespace, matches[j].Name)
	})
	return matches
}

func selectorMatches(labels map[string]string, selector map[string]string) bool {
	for key, value := range selector {
		if labels[key] != value {
			return false
		}
	}
	return true
}

func firstNonEmpty(values []string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func labelOrDefault(label, fallback string) string {
	if label != "" {
		return label
	}
	return fallback
}

func safeID(value string) string {
	value = strings.ToLower(value)
	replacer := strings.NewReplacer("/", "-", ":", "-", ".", "-", "_", "-", " ", "-")
	return replacer.Replace(value)
}
