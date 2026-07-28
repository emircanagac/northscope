package k8s

import (
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
)

type scopedTopologyInputs struct {
	services       []*corev1.Service
	pods           []*corev1.Pod
	nodes          []*corev1.Node
	endpoints      []*corev1.Endpoints
	endpointSlices []*discoveryv1.EndpointSlice
}

func scopeTrafficInputs(
	ingresses []*networkingv1.Ingress,
	services []*corev1.Service,
	pods []*corev1.Pod,
	nodes []*corev1.Node,
	externalResources []ExternalResource,
	endpoints []*corev1.Endpoints,
	endpointSlices []*discoveryv1.EndpointSlice,
) scopedTopologyInputs {
	serviceKeys := make(map[string]struct{})
	for _, ingress := range ingresses {
		for _, route := range ingressRoutes(ingress) {
			if route.ServiceName != "" {
				serviceKeys[namespacedKey(ingress.Namespace, route.ServiceName)] = struct{}{}
			}
		}
	}
	for _, resource := range externalResources {
		for _, backend := range resource.Backends {
			if !isServiceBackend(backend) {
				continue
			}
			namespace := backend.Namespace
			if namespace == "" {
				namespace = resource.Namespace
			}
			serviceKeys[namespacedKey(namespace, backend.Name)] = struct{}{}
		}
		if len(resource.Selector) == 0 {
			continue
		}
		for _, service := range services {
			if (resource.Namespace == "" || resource.Namespace == service.Namespace) &&
				selectorMatches(service.Labels, resource.Selector) {
				serviceKeys[namespacedKey(service.Namespace, service.Name)] = struct{}{}
			}
		}
	}

	// Controller Services are not referenced by an Ingress rule. Keep the
	// bounded set of externally exposed Services so controller inference and
	// NodePort/LB topology remain available without processing every Service.
	for _, service := range services {
		if service.Spec.Type == corev1.ServiceTypeNodePort || service.Spec.Type == corev1.ServiceTypeLoadBalancer {
			serviceKeys[namespacedKey(service.Namespace, service.Name)] = struct{}{}
		}
	}

	scoped := scopedTopologyInputs{}
	servicesByNamespace := make(map[string][]*corev1.Service)
	for _, service := range services {
		if _, ok := serviceKeys[namespacedKey(service.Namespace, service.Name)]; !ok {
			continue
		}
		scoped.services = append(scoped.services, service)
		servicesByNamespace[service.Namespace] = append(servicesByNamespace[service.Namespace], service)
	}

	referencedPods := make(map[string]struct{})
	referencedPodIPs := make(map[string]struct{})
	for _, endpointSlice := range endpointSlices {
		serviceName, ok := endpointSliceServiceName(endpointSlice)
		if !ok {
			continue
		}
		if _, ok := serviceKeys[namespacedKey(endpointSlice.Namespace, serviceName)]; !ok {
			continue
		}
		scoped.endpointSlices = append(scoped.endpointSlices, endpointSlice)
		for _, endpoint := range endpointSlice.Endpoints {
			if endpoint.TargetRef != nil && endpoint.TargetRef.Kind == "Pod" && endpoint.TargetRef.Name != "" {
				namespace := endpoint.TargetRef.Namespace
				if namespace == "" {
					namespace = endpointSlice.Namespace
				}
				referencedPods[namespacedKey(namespace, endpoint.TargetRef.Name)] = struct{}{}
			}
			for _, address := range endpoint.Addresses {
				referencedPodIPs[namespacedKey(endpointSlice.Namespace, address)] = struct{}{}
			}
		}
	}
	for _, endpoint := range endpoints {
		if _, ok := serviceKeys[namespacedKey(endpoint.Namespace, endpoint.Name)]; !ok {
			continue
		}
		scoped.endpoints = append(scoped.endpoints, endpoint)
		for _, subset := range endpoint.Subsets {
			for _, address := range append(subset.Addresses, subset.NotReadyAddresses...) {
				if address.TargetRef != nil && address.TargetRef.Kind == "Pod" && address.TargetRef.Name != "" {
					namespace := address.TargetRef.Namespace
					if namespace == "" {
						namespace = endpoint.Namespace
					}
					referencedPods[namespacedKey(namespace, address.TargetRef.Name)] = struct{}{}
				}
				if address.IP != "" {
					referencedPodIPs[namespacedKey(endpoint.Namespace, address.IP)] = struct{}{}
				}
			}
		}
	}

	nodeNames := make(map[string]struct{})
	for _, pod := range pods {
		if !podBelongsToTrafficService(pod, servicesByNamespace) {
			if _, ok := referencedPods[namespacedKey(pod.Namespace, pod.Name)]; !ok {
				if _, ok := referencedPodIPs[namespacedKey(pod.Namespace, pod.Status.PodIP)]; !ok || pod.Status.PodIP == "" {
					continue
				}
			}
		}
		scoped.pods = append(scoped.pods, pod)
		if pod.Spec.NodeName != "" {
			nodeNames[pod.Spec.NodeName] = struct{}{}
		}
	}
	for _, node := range nodes {
		if _, ok := nodeNames[node.Name]; ok {
			scoped.nodes = append(scoped.nodes, node)
		}
	}
	return scoped
}

func podBelongsToTrafficService(pod *corev1.Pod, servicesByNamespace map[string][]*corev1.Service) bool {
	for _, service := range servicesByNamespace[pod.Namespace] {
		if len(service.Spec.Selector) > 0 && selectorMatches(pod.Labels, service.Spec.Selector) {
			return true
		}
	}
	return false
}
