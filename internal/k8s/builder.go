package k8s

import (
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
	return BuildTopologyWithResourcesAndEndpoints(ingresses, ingressClasses, services, pods, nil, nil, nil, flattenEndpointSlices(endpointSlices))
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
	return BuildTopologyWithResourcesAndEndpoints(ingresses, ingressClasses, services, pods, nodes, externalResources, nil, flattenEndpointSlices(endpointSlices))
}

func BuildTopologyWithResourcesAndEndpoints(
	ingresses []*networkingv1.Ingress,
	ingressClasses []*networkingv1.IngressClass,
	services []*corev1.Service,
	pods []*corev1.Pod,
	nodes []*corev1.Node,
	externalResources []ExternalResource,
	endpoints []*corev1.Endpoints,
	endpointSlices []*discoveryv1.EndpointSlice,
) models.TopologySnapshot {
	builder := newTopologyBuilder()
	ingressClassesByName := make(map[string]*networkingv1.IngressClass, len(ingressClasses))
	servicesByKey := make(map[string]*corev1.Service, len(services))
	nodesByName := make(map[string]*corev1.Node, len(nodes))
	podsByNamespace := make(map[string][]*corev1.Pod)
	podsByName := make(map[string]*corev1.Pod, len(pods))
	podsByIP := make(map[string]*corev1.Pod, len(pods))
	servicePodsCache := make(map[string][]*corev1.Pod, len(services))
	endpointStatsByService := collectEndpointStats(endpointSlices)
	endpointSliceServiceKeys := servicesWithEndpointSlices(endpointSlices)
	mergeEndpointStats(endpointStatsByService, collectLegacyEndpointStats(endpoints), endpointSliceServiceKeys)

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
		if svc.Spec.Type == corev1.ServiceTypeExternalName && svc.Spec.ExternalName != "" {
			builder.addExternalNameEndpoint(svc)
			builder.addEdge(
				nodeID(models.NodeKindService, svc.Namespace, svc.Name),
				externalNameNodeID(svc.Namespace, svc.Name),
				"externalname",
				"ExternalName",
			)
		}
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

	for _, endpointSlice := range endpointSlices {
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

	for _, endpoint := range endpoints {
		svc, ok := servicesByKey[namespacedKey(endpoint.Namespace, endpoint.Name)]
		if !ok || len(svc.Spec.Selector) > 0 {
			continue
		}
		if _, hasEndpointSlice := endpointSliceServiceKeys[namespacedKey(endpoint.Namespace, endpoint.Name)]; hasEndpointSlice {
			continue
		}
		for _, pod := range endpointPods(endpoint, podsByName, podsByIP) {
			builder.addEdge(
				nodeID(models.NodeKindService, svc.Namespace, svc.Name),
				nodeID(models.NodeKindPod, pod.Namespace, pod.Name),
				"endpoint",
				"Endpoints",
			)
		}
		for _, address := range endpointAddresses(endpoint) {
			if endpointAddressReferencesPod(address.Address, endpoint.Namespace, podsByName, podsByIP) {
				continue
			}
			builder.addLegacyEndpoint(endpoint, address)
			builder.addEdge(
				nodeID(models.NodeKindService, svc.Namespace, svc.Name),
				legacyEndpointNodeID(endpoint.Namespace, endpoint.Name, address.Address.IP),
				"endpoint",
				"Endpoints",
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
