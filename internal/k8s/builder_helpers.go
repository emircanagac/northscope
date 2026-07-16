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
	case models.NodeKindEndpoint:
		return 1110
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

func legacyEndpointNodeID(namespace, serviceName, address string) string {
	return nodeID(models.NodeKindEndpoint, namespace, serviceName+":endpoints:"+safeID(address))
}

func externalNameNodeID(namespace, serviceName string) string {
	return nodeID(models.NodeKindEndpoint, namespace, serviceName+":externalname")
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
