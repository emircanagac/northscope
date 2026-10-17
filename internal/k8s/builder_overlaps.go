package k8s

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	networkingv1 "k8s.io/api/networking/v1"
)

const (
	nginxCanaryAnnotation              = "nginx.ingress.kubernetes.io/canary"
	nginxCanaryWeightAnnotation        = "nginx.ingress.kubernetes.io/canary-weight"
	nginxCanaryWeightTotalAnnotation   = "nginx.ingress.kubernetes.io/canary-weight-total"
	nginxCanaryByHeaderAnnotation      = "nginx.ingress.kubernetes.io/canary-by-header"
	nginxCanaryByHeaderValueAnnotation = "nginx.ingress.kubernetes.io/canary-by-header-value"
	nginxCanaryByHeaderPatternAnnot    = "nginx.ingress.kubernetes.io/canary-by-header-pattern"
	nginxCanaryByCookieAnnotation      = "nginx.ingress.kubernetes.io/canary-by-cookie"
)

// routeOverlapKey identifies the traffic one controller matches: Ingresses
// handled by different controllers can reuse a host and path without
// competing for the same requests.
type routeOverlapKey struct {
	controllerID string
	host         string
	path         string
	pathType     string
}

type overlapRoute struct {
	ingress *networkingv1.Ingress
	route   ingressRoute
	key     routeOverlapKey
	canary  bool
	rule    string
}

func (r overlapRoute) ingressName() string {
	return displayName(r.ingress.Namespace, r.ingress.Name)
}

func (r overlapRoute) same(other overlapRoute) bool {
	return r.ingress == other.ingress && r.route.ID == other.route.ID
}

// backendKey identifies the upstream ingress-nginx builds for a route: one
// per controller, namespace, Service, and Service port.
func (r overlapRoute) backendKey() string {
	if r.route.ServiceName == "" {
		return ""
	}
	return strings.Join([]string{r.key.controllerID, r.ingress.Namespace, r.route.ServiceName, r.route.ServicePortLabel()}, "|")
}

// canaryLink is a canary Ingress that ingress-nginx attaches to the backend
// of Primary, the Ingress it serves for the canary's host and path.
type canaryLink struct {
	Canary  overlapRoute
	Primary overlapRoute
}

// routeOverlap describes how other Ingresses for the same controller affect
// one route.
type routeOverlap struct {
	// Primaries are the other non-canary Ingresses for this host and path.
	Primaries []overlapRoute
	// LocationCanaries are the other canary Ingresses for this host and path.
	LocationCanaries []overlapRoute
	// Canaries split the traffic of this route's Service. ingress-nginx
	// attaches a canary to the primary's backend Service, so every route to
	// that Service and port receives the split, whatever its host.
	Canaries []canaryLink
	// SharedCanaryService lists other canary Ingresses that send traffic to
	// the same canary Service, whose rules ingress-nginx merges into one.
	SharedCanaryService []overlapRoute
	// Oldest is the non-canary Ingress ingress-nginx serves when several
	// define the same location.
	Oldest  *overlapRoute
	Canary  bool
	Rule    string
	HasSelf bool
}

// collectRouteOverlaps groups Ingress routes by controller, host, path, and
// path type, links ingress-nginx canaries to the backends they split, and
// returns the overlap for every route node that is affected.
func collectRouteOverlaps(ingresses []*networkingv1.Ingress, controllerIDs map[string]string) map[string]routeOverlap {
	groups := map[routeOverlapKey][]overlapRoute{}
	var keys []routeOverlapKey
	for _, ingress := range ingresses {
		controllerID := controllerIDs[namespacedKey(ingress.Namespace, ingress.Name)]
		if controllerID == "" {
			continue
		}
		rule, canary := nginxCanaryRule(ingress)
		seen := map[routeOverlapKey]struct{}{}
		for _, route := range ingressRoutes(ingress) {
			if route.IsDefault {
				continue
			}
			key := routeOverlapKey{
				controllerID: controllerID,
				host:         strings.ToLower(route.Host),
				path:         routePathOrRoot(route.Path),
				pathType:     routePathTypeOrDefault(route.PathType),
			}
			// One Ingress listing the same location twice is not an overlap
			// between Ingresses.
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			if _, ok := groups[key]; !ok {
				keys = append(keys, key)
			}
			groups[key] = append(groups[key], overlapRoute{ingress: ingress, route: route, key: key, canary: canary, rule: rule})
		}
	}

	oldestByKey := map[routeOverlapKey]*overlapRoute{}
	canariesByBackend := map[string][]canaryLink{}
	canariesByService := map[string][]overlapRoute{}
	for _, key := range keys {
		var primaries, canaries []overlapRoute
		for _, member := range groups[key] {
			if member.canary {
				canaries = append(canaries, member)
			} else {
				primaries = append(primaries, member)
			}
		}
		oldest := oldestOverlapRoute(primaries)
		oldestByKey[key] = oldest
		for _, canary := range canaries {
			if serviceKey := canary.backendKey(); serviceKey != "" {
				canariesByService[serviceKey] = append(canariesByService[serviceKey], canary)
			}
			if oldest != nil {
				if backend := oldest.backendKey(); backend != "" {
					canariesByBackend[backend] = append(canariesByBackend[backend], canaryLink{Canary: canary, Primary: *oldest})
				}
			}
		}
	}

	overlaps := map[string]routeOverlap{}
	for _, key := range keys {
		members := groups[key]
		for _, member := range members {
			overlap := routeOverlap{Canary: member.canary, Rule: member.rule, Oldest: oldestByKey[key]}
			for _, other := range members {
				if other.same(member) {
					continue
				}
				if other.canary {
					overlap.LocationCanaries = append(overlap.LocationCanaries, other)
				} else {
					overlap.Primaries = append(overlap.Primaries, other)
				}
			}
			if member.canary {
				for _, other := range canariesByService[member.backendKey()] {
					if other.ingress != member.ingress {
						overlap.SharedCanaryService = append(overlap.SharedCanaryService, other)
					}
				}
			} else if backend := member.backendKey(); backend != "" {
				overlap.Canaries = canariesByBackend[backend]
			}
			overlap.HasSelf = member.canary || len(overlap.Primaries) > 0 || len(overlap.LocationCanaries) > 0 || len(overlap.Canaries) > 0
			if overlap.HasSelf {
				overlaps[ingressRouteNodeID(member.ingress.Namespace, member.ingress.Name, member.route.ID)] = overlap
			}
		}
	}
	return overlaps
}

// oldestOverlapRoute returns the Ingress ingress-nginx keeps for a duplicated
// location: it sorts Ingresses by creation time (then namespace and name) and
// ignores later definitions of a location it has already configured.
func oldestOverlapRoute(routes []overlapRoute) *overlapRoute {
	if len(routes) == 0 {
		return nil
	}
	sorted := append([]overlapRoute(nil), routes...)
	sort.SliceStable(sorted, func(i, j int) bool {
		left, right := sorted[i].ingress, sorted[j].ingress
		if !left.CreationTimestamp.Equal(&right.CreationTimestamp) {
			return left.CreationTimestamp.Before(&right.CreationTimestamp)
		}
		return namespacedKey(left.Namespace, left.Name) < namespacedKey(right.Namespace, right.Name)
	})
	return &sorted[0]
}

// nginxCanaryRule reports whether an Ingress is an ingress-nginx canary and
// summarizes which requests it receives, in the order ingress-nginx applies
// the rules: header, then cookie, then weight.
func nginxCanaryRule(ingress *networkingv1.Ingress) (string, bool) {
	annotations := ingress.Annotations
	if !strings.EqualFold(strings.TrimSpace(annotations[nginxCanaryAnnotation]), "true") {
		return "", false
	}
	var rules []string
	if header := strings.TrimSpace(annotations[nginxCanaryByHeaderAnnotation]); header != "" {
		switch {
		case annotations[nginxCanaryByHeaderValueAnnotation] != "":
			rules = append(rules, fmt.Sprintf("header %s: %s", header, annotations[nginxCanaryByHeaderValueAnnotation]))
		case annotations[nginxCanaryByHeaderPatternAnnot] != "":
			rules = append(rules, fmt.Sprintf("header %s matching %s", header, annotations[nginxCanaryByHeaderPatternAnnot]))
		default:
			rules = append(rules, fmt.Sprintf("header %s: always", header))
		}
	}
	if cookie := strings.TrimSpace(annotations[nginxCanaryByCookieAnnotation]); cookie != "" {
		rules = append(rules, fmt.Sprintf("cookie %s: always", cookie))
	}
	if weight := strings.TrimSpace(annotations[nginxCanaryWeightAnnotation]); weight != "" {
		rules = append(rules, "weight "+canaryWeightLabel(weight, annotations[nginxCanaryWeightTotalAnnotation]))
	}
	if len(rules) == 0 {
		return "no canary rule, so it receives no traffic", true
	}
	return strings.Join(rules, ", "), true
}

func canaryWeightLabel(weight, total string) string {
	value, err := strconv.Atoi(weight)
	if err != nil {
		return weight
	}
	totalValue := 100
	if parsed, err := strconv.Atoi(strings.TrimSpace(total)); err == nil && parsed > 0 {
		totalValue = parsed
	}
	if totalValue == 100 {
		return strconv.Itoa(value) + "%"
	}
	return fmt.Sprintf("%d/%d", value, totalValue)
}

func routePathOrRoot(path string) string {
	if path == "" {
		return "/"
	}
	return path
}

func routePathTypeOrDefault(pathType string) string {
	if pathType == "" {
		return string(networkingv1.PathTypeImplementationSpecific)
	}
	return pathType
}

func routeLocationLabel(route ingressRoute) string {
	host := route.Host
	if host == "" {
		host = "*"
	}
	return host + routePathOrRoot(route.Path)
}

func overlapIngressNames(routes []overlapRoute) string {
	names := make([]string, 0, len(routes))
	for _, route := range routes {
		names = append(names, route.ingressName())
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// applyRouteOverlap adds conflict and canary findings to a route diagnosis.
// Existing errors and warnings stay first because they block traffic on
// their own; the overlap is still reported through route properties.
func applyRouteOverlap(diagnosis routeDiagnosis, ingress *networkingv1.Ingress, route ingressRoute, overlap routeOverlap) routeDiagnosis {
	if !overlap.HasSelf {
		return diagnosis
	}
	healthy := diagnosis.Severity == "ok" || diagnosis.Severity == "info"
	location := routeLocationLabel(route)
	kubectl := fmt.Sprintf("kubectl get ingress -A -o wide | grep %s", strings.ToLower(route.Host))
	if route.Host == "" {
		kubectl = "kubectl get ingress -A -o wide"
	}

	if overlap.Canary {
		switch {
		case len(overlap.Primaries) == 0 && healthy:
			return routeDiagnosis{
				Status:     "Warning",
				Severity:   "warning",
				Message:    fmt.Sprintf("Canary Ingress for %s has no primary Ingress with the same host and path, so ingress-nginx does not route traffic to it.", location),
				NextStep:   "Create the primary Ingress for this host and path, or remove the canary annotation.",
				Kubectl:    kubectl,
				Confidence: "Inferred",
			}
		case len(overlap.SharedCanaryService) > 0 && healthy:
			return routeDiagnosis{
				Status:     "Conflict",
				Severity:   "warning",
				Message:    fmt.Sprintf("Canary Service %q is also used by %s; ingress-nginx keeps one traffic rule per canary Service, so their header, cookie, and weight rules override each other.", route.ServiceName, overlapIngressNames(overlap.SharedCanaryService)),
				NextStep:   "Give each canary Ingress its own Service.",
				Kubectl:    fmt.Sprintf("kubectl get ingress -n %s -o wide", ingress.Namespace),
				Confidence: "Inferred",
			}
		case len(overlap.LocationCanaries) > 0 && healthy:
			return routeDiagnosis{
				Status:     "Conflict",
				Severity:   "warning",
				Message:    fmt.Sprintf("Several canary Ingresses define %s (also %s); ingress-nginx uses only one canary per host and path.", location, overlapIngressNames(overlap.LocationCanaries)),
				NextStep:   "Keep a single canary Ingress for this host and path.",
				Kubectl:    kubectl,
				Confidence: "Inferred",
			}
		}
		return diagnosis
	}

	if len(overlap.Primaries) > 0 && healthy {
		served := ""
		if overlap.Oldest != nil {
			if overlap.Oldest.ingress == ingress {
				served = " ingress-nginx serves this Ingress because it is the oldest;"
			} else {
				served = fmt.Sprintf(" ingress-nginx serves the oldest, %s;", overlap.Oldest.ingressName())
			}
		}
		return routeDiagnosis{
			Status:     "Conflict",
			Severity:   "warning",
			Message:    fmt.Sprintf("%s is also defined by %s for the same controller, so only one of their backends receives this traffic.", location, overlapIngressNames(overlap.Primaries)),
			NextStep:   strings.TrimSpace(fmt.Sprintf("Keep one Ingress per host and path, or mark the extra one as a canary.%s other controllers may choose differently.", served)),
			Kubectl:    kubectl,
			Confidence: "Configured",
		}
	}
	return diagnosis
}

// overlapProperties exposes the overlap on the route node so the UI can show
// it even when another finding takes precedence in the diagnosis.
func overlapProperties(overlap routeOverlap, ingress *networkingv1.Ingress, route ingressRoute) map[string]string {
	if !overlap.HasSelf {
		return nil
	}
	properties := map[string]string{}
	if overlap.Canary {
		properties["canary"] = "true"
		properties["canaryRule"] = overlap.Rule
		if len(overlap.Primaries) > 0 {
			properties["canaryOf"] = overlapIngressNames(overlap.Primaries)
		}
		if len(overlap.SharedCanaryService) > 0 {
			properties["canaryServiceSharedWith"] = overlapIngressNames(overlap.SharedCanaryService)
		}
		return properties
	}
	if len(overlap.Primaries) > 0 {
		properties["conflictsWith"] = overlapIngressNames(overlap.Primaries)
	}
	if len(overlap.Canaries) > 0 {
		parts := make([]string, 0, len(overlap.Canaries))
		for _, link := range overlap.Canaries {
			part := fmt.Sprintf("%s -> %s (%s)", link.Canary.ingressName(), link.Canary.route.BackendLabel(), link.Canary.rule)
			// A canary for another host still splits this route because
			// ingress-nginx attaches it to the shared Service.
			if link.Primary.ingress != ingress || link.Primary.route.ID != route.ID {
				part += fmt.Sprintf(", applied through Service %s shared with %s", route.ServiceName, routeLocationLabel(link.Primary.route))
			}
			parts = append(parts, part)
		}
		sort.Strings(parts)
		properties["canaries"] = strings.Join(parts, "; ")
	}
	return properties
}
