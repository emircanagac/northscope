package k8s

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
)

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
	if service.Spec.Type == corev1.ServiceTypeExternalName && service.Spec.ExternalName != "" {
		return routeDiagnosis{
			Status:     "Healthy",
			Severity:   "ok",
			Message:    fmt.Sprintf("Route resolves to ExternalName Service %q targeting %q.", service.Name, service.Spec.ExternalName),
			NextStep:   "If users still fail, verify external DNS, upstream reachability, TLS, and NetworkPolicy.",
			Kubectl:    fmt.Sprintf("kubectl get svc %s -n %s -o yaml", service.Name, service.Namespace),
			Confidence: "Configured",
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
			Message:    fmt.Sprintf("Selector-less Service %q has no EndpointSlice or Endpoints data.", service.Name),
			NextStep:   "Verify manually managed EndpointSlices, legacy Endpoints, or external backend wiring.",
			Kubectl:    fmt.Sprintf("kubectl get endpointslice -n %s -l kubernetes.io/service-name=%s -o yaml; kubectl get endpoints %s -n %s -o yaml", service.Namespace, service.Name, service.Name, service.Namespace),
			Confidence: "Certain",
		}
	}
	if stats.Total == 0 && len(service.Spec.Selector) > 0 {
		return routeDiagnosis{
			Status:     "Warning",
			Severity:   "warning",
			Message:    fmt.Sprintf("Service %q has Ready Pods, but no EndpointSlice or Endpoints data was observed.", service.Name),
			NextStep:   "Check EndpointSlice/Endpoints RBAC and the endpoint controllers if traffic still fails.",
			Kubectl:    fmt.Sprintf("kubectl get endpointslice -n %s -l kubernetes.io/service-name=%s; kubectl get endpoints %s -n %s", service.Namespace, service.Name, service.Name, service.Namespace),
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
		Kubectl:    fmt.Sprintf("kubectl describe ingress %s -n %s; kubectl get endpointslice -n %s -l kubernetes.io/service-name=%s -o wide; kubectl get endpoints %s -n %s", ingress.Name, ingress.Namespace, service.Namespace, service.Name, service.Name, service.Namespace),
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
