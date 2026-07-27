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

const defaultIngressClassAnnotation = "ingressclass.kubernetes.io/is-default-class"

func ingressControllerClassName(ingress *networkingv1.Ingress, ingressClassesByName map[string]*networkingv1.IngressClass) string {
	className := ingressClassName(ingress)
	if className != "" {
		if _, ok := ingressClassesByName[className]; ok {
			return className
		}
		return ""
	}
	if className := defaultIngressClassName(ingressClassesByName); className != "" {
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
	if len(ingressClassesByName) == 0 ||
		(service.Spec.Type != corev1.ServiceTypeNodePort && service.Spec.Type != corev1.ServiceTypeLoadBalancer) {
		return ""
	}

	serviceText := strings.ToLower(strings.Join(serviceIdentityTerms(service, pods), " "))
	bestScore := 0
	bestName := ""
	bestTied := false
	for name, ingressClass := range ingressClassesByName {
		score := ingressControllerServiceScore(service, serviceText, name, ingressClass.Spec.Controller)
		if score > bestScore {
			bestScore = score
			bestName = name
			bestTied = false
		} else if score > 0 && score == bestScore {
			bestTied = true
		}
	}

	if bestScore < 6 || bestTied {
		return ""
	}
	return bestName
}

func defaultIngressClassName(ingressClassesByName map[string]*networkingv1.IngressClass) string {
	defaultName := ""
	for name, ingressClass := range ingressClassesByName {
		if !strings.EqualFold(ingressClass.Annotations[defaultIngressClassAnnotation], "true") {
			continue
		}
		if defaultName != "" {
			return ""
		}
		defaultName = name
	}
	return defaultName
}

func ingressControllerServiceScore(service *corev1.Service, serviceText, className, controller string) int {
	className = strings.ToLower(className)
	serviceName := strings.ToLower(service.Name)
	score := 0
	matchedIdentity := false

	if className != "" && serviceName == className {
		score += 6
		matchedIdentity = true
	} else if className != "" && strings.Contains(serviceText, className) {
		score += 4
		matchedIdentity = true
	}

	for _, token := range controllerIdentityTokens(controller) {
		if token == className || strings.Contains(className, token) {
			continue
		}
		if strings.Contains(serviceText, token) {
			score += 2
			matchedIdentity = true
		}
	}
	if !matchedIdentity {
		return 0
	}
	if strings.Contains(serviceText, "ingress") {
		score += 2
	}
	if strings.Contains(serviceText, "controller") {
		score += 2
	}
	if strings.EqualFold(service.Labels["app.kubernetes.io/component"], "controller") {
		score += 4
	}
	return score
}

func controllerIdentityTokens(controller string) []string {
	parts := strings.FieldsFunc(strings.ToLower(controller), func(r rune) bool {
		return r < 'a' || r > 'z'
	})
	ignored := map[string]struct{}{
		"com": {}, "io": {}, "org": {}, "k8s": {}, "kubernetes": {}, "ingress": {}, "controller": {},
	}
	seen := map[string]struct{}{}
	tokens := make([]string, 0, len(parts))
	for _, part := range parts {
		if len(part) < 4 {
			continue
		}
		if _, ok := ignored[part]; ok {
			continue
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		tokens = append(tokens, part)
	}
	return tokens
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
