package k8s

import (
	"fmt"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/emircanagac/northscope/internal/models"
)

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
	return strings.Contains(haystack, "ingress") ||
		strings.Contains(haystack, "controller") ||
		strings.Contains(haystack, "nginx") ||
		strings.Contains(haystack, "traefik") ||
		strings.Contains(haystack, "haproxy")
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
