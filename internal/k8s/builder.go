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
		for _, backendName := range ingressBackendServiceNames(ingress) {
			svc, ok := servicesByKey[namespacedKey(ingress.Namespace, backendName)]
			if !ok {
				continue
			}
			builder.addEdge(
				nodeID(models.NodeKindIngress, ingress.Namespace, ingress.Name),
				nodeID(models.NodeKindService, svc.Namespace, svc.Name),
				"routes",
				"HTTP route",
			)
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
			Status:     string(pod.Status.Phase),
			Phase:      string(pod.Status.Phase),
			Metadata:   pod.Labels,
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

func ingressBackendServiceNames(ingress *networkingv1.Ingress) []string {
	seen := map[string]struct{}{}
	add := func(backend networkingv1.IngressBackend) {
		if backend.Service == nil || backend.Service.Name == "" {
			return
		}
		seen[backend.Service.Name] = struct{}{}
	}

	if ingress.Spec.DefaultBackend != nil {
		add(*ingress.Spec.DefaultBackend)
	}

	for _, rule := range ingress.Spec.Rules {
		if rule.HTTP == nil {
			continue
		}
		for _, path := range rule.HTTP.Paths {
			add(path.Backend)
		}
	}

	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func ingressClassName(ingress *networkingv1.Ingress) string {
	if ingress.Spec.IngressClassName != nil {
		return *ingress.Spec.IngressClassName
	}
	return ingress.Annotations["kubernetes.io/ingress.class"]
}

func ingressStatus(ingress *networkingv1.Ingress) string {
	if len(ingress.Status.LoadBalancer.Ingress) > 0 {
		return "Active"
	}
	return "Pending"
}

func serviceStatus(service *corev1.Service) string {
	if service.Spec.Type == corev1.ServiceTypeLoadBalancer && len(service.Status.LoadBalancer.Ingress) == 0 {
		return "Pending"
	}
	return "Active"
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

func ingressLoadBalancerNodeID(namespace, name, address string) string {
	return nodeID(models.NodeKindLoadBalancer, namespace, "ingress:"+name+":"+safeID(address))
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
