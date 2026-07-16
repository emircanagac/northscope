package k8s

import (
	"sort"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"

	"github.com/emircanagac/northscope/internal/models"
)

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
	if service.Spec.Type == corev1.ServiceTypeExternalName && service.Spec.ExternalName != "" {
		properties["externalName"] = service.Spec.ExternalName
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

func (b *topologyBuilder) addLegacyEndpoint(endpoints *corev1.Endpoints, address legacyEndpointAddress) {
	if address.Address.IP == "" {
		return
	}

	status := "Ready"
	if !address.Ready {
		status = "NotReady"
	}
	properties := map[string]string{
		"address":      address.Address.IP,
		"endpointType": "external",
		"service":      displayName(endpoints.Namespace, endpoints.Name),
		"source":       "Endpoints",
	}
	if address.Address.TargetRef != nil {
		properties["targetRef"] = strings.Trim(address.Address.TargetRef.Kind+"/"+address.Address.TargetRef.Name, "/")
	}

	b.addNode(models.Node{
		ID:       legacyEndpointNodeID(endpoints.Namespace, endpoints.Name, address.Address.IP),
		Type:     "northscopeNode",
		Position: b.nextPosition(models.NodeKindEndpoint),
		Data: models.NodeData{
			Label:      address.Address.IP,
			Kind:       models.NodeKindEndpoint,
			Namespace:  endpoints.Namespace,
			Name:       address.Address.IP,
			Status:     status,
			Properties: properties,
		},
	})
}

func (b *topologyBuilder) addExternalNameEndpoint(service *corev1.Service) {
	properties := map[string]string{
		"externalName": service.Spec.ExternalName,
		"endpointType": "externalName",
		"service":      displayName(service.Namespace, service.Name),
	}
	if len(service.Spec.Ports) > 0 {
		properties["ports"] = servicePorts(service)
	}

	b.addNode(models.Node{
		ID:       externalNameNodeID(service.Namespace, service.Name),
		Type:     "northscopeNode",
		Position: b.nextPosition(models.NodeKindEndpoint),
		Data: models.NodeData{
			Label:      service.Spec.ExternalName,
			Kind:       models.NodeKindEndpoint,
			Namespace:  service.Namespace,
			Name:       service.Spec.ExternalName,
			Status:     "ExternalName",
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
