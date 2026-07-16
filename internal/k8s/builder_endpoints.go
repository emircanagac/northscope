package k8s

import (
	"sort"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
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
