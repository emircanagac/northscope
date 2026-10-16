package k8s

import (
	"sort"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
)

type endpointStats struct {
	Total       int
	Usable      int
	Ready       int
	Serving     int
	Terminating int
	Slices      []string
}

type legacyEndpointAddress struct {
	Address corev1.EndpointAddress
	Ready   bool
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

func collectLegacyEndpointStats(endpoints []*corev1.Endpoints) map[string]endpointStats {
	statsByService := map[string]endpointStats{}
	for _, item := range endpoints {
		key := namespacedKey(item.Namespace, item.Name)
		stats := statsByService[key]
		stats.Slices = append(stats.Slices, item.Name)
		for _, subset := range item.Subsets {
			for range subset.Addresses {
				stats.Total++
				stats.Ready++
				stats.Serving++
				stats.Usable++
			}
			for range subset.NotReadyAddresses {
				stats.Total++
			}
		}
		sort.Strings(stats.Slices)
		statsByService[key] = stats
	}
	return statsByService
}

func mergeEndpointStats(target, source map[string]endpointStats, skipKeys map[string]struct{}) {
	for key, incoming := range source {
		if _, skip := skipKeys[key]; skip {
			continue
		}
		current := target[key]
		current.Total += incoming.Total
		current.Usable += incoming.Usable
		current.Ready += incoming.Ready
		current.Serving += incoming.Serving
		current.Terminating += incoming.Terminating
		current.Slices = append(current.Slices, incoming.Slices...)
		sort.Strings(current.Slices)
		target[key] = current
	}
}

func servicesWithEndpointSlices(endpointSlices []*discoveryv1.EndpointSlice) map[string]struct{} {
	services := map[string]struct{}{}
	for _, endpointSlice := range endpointSlices {
		serviceName, ok := endpointSliceServiceName(endpointSlice)
		if !ok {
			continue
		}
		services[namespacedKey(endpointSlice.Namespace, serviceName)] = struct{}{}
	}
	return services
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

func endpointPods(endpoints *corev1.Endpoints, podsByName, podsByIP map[string]*corev1.Pod) []*corev1.Pod {
	seen := map[string]*corev1.Pod{}
	for _, address := range endpointAddresses(endpoints) {
		if address.Address.TargetRef != nil && address.Address.TargetRef.Kind == "Pod" && address.Address.TargetRef.Name != "" {
			namespace := address.Address.TargetRef.Namespace
			if namespace == "" {
				namespace = endpoints.Namespace
			}
			if pod, ok := podsByName[namespacedKey(namespace, address.Address.TargetRef.Name)]; ok {
				seen[namespacedKey(pod.Namespace, pod.Name)] = pod
				continue
			}
		}

		if pod, ok := podsByIP[namespacedKey(endpoints.Namespace, address.Address.IP)]; ok {
			seen[namespacedKey(pod.Namespace, pod.Name)] = pod
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

func endpointAddresses(endpoints *corev1.Endpoints) []legacyEndpointAddress {
	var addresses []legacyEndpointAddress
	for _, subset := range endpoints.Subsets {
		for _, address := range subset.Addresses {
			addresses = append(addresses, legacyEndpointAddress{Address: address, Ready: true})
		}
		for _, address := range subset.NotReadyAddresses {
			addresses = append(addresses, legacyEndpointAddress{Address: address})
		}
	}
	return addresses
}

func endpointAddressReferencesPod(address corev1.EndpointAddress, namespace string, podsByName, podsByIP map[string]*corev1.Pod) bool {
	if address.TargetRef != nil && address.TargetRef.Kind == "Pod" && address.TargetRef.Name != "" {
		targetNamespace := address.TargetRef.Namespace
		if targetNamespace == "" {
			targetNamespace = namespace
		}
		if _, ok := podsByName[namespacedKey(targetNamespace, address.TargetRef.Name)]; ok {
			return true
		}
	}
	_, ok := podsByIP[namespacedKey(namespace, address.IP)]
	return ok
}

// endpointPortPods maps a Service port name to the Pods that are endpoints for
// that port.
type endpointPortPods map[string]map[string]struct{}

// endpointPodsByService maps each Service that has endpoint data to the Pods
// its EndpointSlices, or its legacy Endpoints when it has no EndpointSlices,
// list for each Service port. A Pod that lacks a named targetPort still appears
// in an EndpointSlice, but without that port, so membership is tracked per port.
func endpointPodsByService(
	endpointSlices []*discoveryv1.EndpointSlice,
	endpoints []*corev1.Endpoints,
	endpointSliceServiceKeys map[string]struct{},
	podsByName map[string]*corev1.Pod,
	podsByIP map[string]*corev1.Pod,
) map[string]endpointPortPods {
	result := map[string]endpointPortPods{}
	add := func(serviceKey string, portNames []string, pods []*corev1.Pod) {
		ports := result[serviceKey]
		if ports == nil {
			ports = endpointPortPods{}
			result[serviceKey] = ports
		}
		for _, portName := range portNames {
			set := ports[portName]
			if set == nil {
				set = map[string]struct{}{}
				ports[portName] = set
			}
			for _, pod := range pods {
				set[namespacedKey(pod.Namespace, pod.Name)] = struct{}{}
			}
		}
	}
	for _, endpointSlice := range endpointSlices {
		serviceName, ok := endpointSliceServiceName(endpointSlice)
		if !ok {
			continue
		}
		portNames := make([]string, 0, len(endpointSlice.Ports))
		for _, port := range endpointSlice.Ports {
			name := ""
			if port.Name != nil {
				name = *port.Name
			}
			portNames = append(portNames, name)
		}
		add(namespacedKey(endpointSlice.Namespace, serviceName), portNames, endpointSlicePods(endpointSlice, podsByName, podsByIP))
	}
	for _, item := range endpoints {
		serviceKey := namespacedKey(item.Namespace, item.Name)
		if _, ok := endpointSliceServiceKeys[serviceKey]; ok {
			continue
		}
		add(serviceKey, nil, nil)
		for _, subset := range item.Subsets {
			portNames := make([]string, 0, len(subset.Ports))
			for _, port := range subset.Ports {
				portNames = append(portNames, port.Name)
			}
			subsetOnly := &corev1.Endpoints{ObjectMeta: item.ObjectMeta, Subsets: []corev1.EndpointSubset{subset}}
			add(serviceKey, portNames, endpointPods(subsetOnly, podsByName, podsByIP))
		}
	}
	return result
}

// countNonEndpointPods counts running Pods with an IP that a Service selects
// but that are not endpoints for the given Service port, for example because
// they lack the named targetPort. It returns 0 when the Service has no
// endpoint data.
func countNonEndpointPods(selected []*corev1.Pod, ports endpointPortPods, portName string) int {
	if ports == nil {
		return 0
	}
	members := ports[portName]
	count := 0
	for _, pod := range selected {
		if pod.Status.PodIP == "" || pod.Status.Phase != corev1.PodRunning || pod.DeletionTimestamp != nil {
			continue
		}
		if _, ok := members[namespacedKey(pod.Namespace, pod.Name)]; !ok {
			count++
		}
	}
	return count
}

// servicePortName returns the name of the Service port an Ingress backend
// refers to, and false when the Service has no such port.
func servicePortName(service *corev1.Service, backendPort networkingv1.ServiceBackendPort) (string, bool) {
	for _, port := range service.Spec.Ports {
		if (backendPort.Name != "" && port.Name == backendPort.Name) || (backendPort.Number != 0 && port.Port == backendPort.Number) {
			return port.Name, true
		}
	}
	return "", false
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
