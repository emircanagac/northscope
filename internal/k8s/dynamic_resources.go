package k8s

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

type optionalResource struct {
	gvr   schema.GroupVersionResource
	scope string
	kind  ExternalResourceKind
}

var optionalTopologyResources = []optionalResource{
	{gvr: schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "gatewayclasses"}, scope: "cluster", kind: ExternalKindGatewayClass},
	{gvr: schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1beta1", Resource: "gatewayclasses"}, scope: "cluster", kind: ExternalKindGatewayClass},
	{gvr: schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "gateways"}, scope: "namespace", kind: ExternalKindGateway},
	{gvr: schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1beta1", Resource: "gateways"}, scope: "namespace", kind: ExternalKindGateway},
	{gvr: schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "httproutes"}, scope: "namespace", kind: ExternalKindHTTPRoute},
	{gvr: schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1beta1", Resource: "httproutes"}, scope: "namespace", kind: ExternalKindHTTPRoute},
	{gvr: schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "grpcroutes"}, scope: "namespace", kind: ExternalKindGRPCRoute},
	{gvr: schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "tlsroutes"}, scope: "namespace", kind: ExternalKindTLSRoute},
	{gvr: schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1alpha2", Resource: "tlsroutes"}, scope: "namespace", kind: ExternalKindTLSRoute},
	{gvr: schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "tcproutes"}, scope: "namespace", kind: ExternalKindTCPRoute},
	{gvr: schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1alpha2", Resource: "tcproutes"}, scope: "namespace", kind: ExternalKindTCPRoute},
	{gvr: schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "udproutes"}, scope: "namespace", kind: ExternalKindUDPRoute},
	{gvr: schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1alpha2", Resource: "udproutes"}, scope: "namespace", kind: ExternalKindUDPRoute},
	{gvr: schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "referencegrants"}, scope: "namespace", kind: ExternalKindReferenceGrant},
	{gvr: schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1beta1", Resource: "referencegrants"}, scope: "namespace", kind: ExternalKindReferenceGrant},
	{gvr: schema.GroupVersionResource{Group: "cis.f5.com", Version: "v1", Resource: "ingresslinks"}, scope: "namespace", kind: ExternalKindF5IngressLink},
	{gvr: schema.GroupVersionResource{Group: "cis.f5.com", Version: "v1", Resource: "virtualservers"}, scope: "namespace", kind: ExternalKindF5Virtual},
	{gvr: schema.GroupVersionResource{Group: "cis.f5.com", Version: "v1", Resource: "transportservers"}, scope: "namespace", kind: ExternalKindF5Transport},
}

func listOptionalExternalResources(
	ctx context.Context,
	client dynamic.Interface,
	availableGVRs map[schema.GroupVersionResource]struct{},
	namespace string,
) ([]ExternalResource, bool) {
	if client == nil {
		return nil, true
	}

	var resources []ExternalResource
	complete := true
	for _, item := range preferredOptionalResources(availableGVRs) {
		list, err := listOptionalResource(ctx, client, item, namespace)
		if err != nil {
			if apierrors.IsNotFound(err) || apierrors.IsForbidden(err) {
				continue
			}
			log.Printf("list optional topology resource %s failed: %v", item.gvr.String(), err)
			complete = false
			continue
		}
		for i := range list.Items {
			if resource, ok := externalResourceFromUnstructured(&list.Items[i], item.kind, item.gvr.GroupVersion().String()); ok {
				resources = append(resources, resource)
			}
		}
	}
	return resources, complete
}

func preferredOptionalResources(availableGVRs map[schema.GroupVersionResource]struct{}) []optionalResource {
	selected := make([]optionalResource, 0, len(optionalTopologyResources))
	seen := make(map[string]struct{})
	for _, item := range optionalTopologyResources {
		if availableGVRs != nil {
			if _, ok := availableGVRs[item.gvr]; !ok {
				continue
			}
		}
		key := item.gvr.Group + "/" + item.gvr.Resource
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		selected = append(selected, item)
	}
	return selected
}

func listOptionalResource(ctx context.Context, client dynamic.Interface, item optionalResource, namespace string) (*unstructured.UnstructuredList, error) {
	if item.scope == "cluster" {
		return client.Resource(item.gvr).List(ctx, metav1.ListOptions{})
	}
	return client.Resource(item.gvr).Namespace(namespace).List(ctx, metav1.ListOptions{})
}

func externalResourceFromUnstructured(obj *unstructured.Unstructured, kind ExternalResourceKind, apiVersion string) (ExternalResource, bool) {
	resource := ExternalResource{
		Kind:       kind,
		APIVersion: apiVersion,
		Namespace:  obj.GetNamespace(),
		Name:       obj.GetName(),
		Properties: map[string]string{},
	}

	switch kind {
	case ExternalKindGatewayClass:
		resource.Properties["controllerName"], _, _ = unstructured.NestedString(obj.Object, "spec", "controllerName")
	case ExternalKindGateway:
		resource.ClassName, _, _ = unstructured.NestedString(obj.Object, "spec", "gatewayClassName")
		resource.Addresses = gatewayAddresses(obj)
		resource.Hostnames = listenerHostnames(obj)
		resource.Listeners = gatewayListeners(obj)
		copyConditionStatus(obj, resource.Properties)
	case ExternalKindHTTPRoute, ExternalKindGRPCRoute, ExternalKindTLSRoute, ExternalKindTCPRoute, ExternalKindUDPRoute:
		resource.Hostnames = stringSlice(obj, "spec", "hostnames")
		resource.Paths = routePaths(obj, kind)
		resource.ParentRefs = parentRefs(obj)
		resource.Backends = routeBackendRefs(obj)
		copyConditionStatus(obj, resource.Properties)
	case ExternalKindReferenceGrant:
		resource.GrantFrom = referenceGrantFrom(obj)
		resource.GrantTo = referenceGrantTo(obj)
	case ExternalKindF5IngressLink:
		resource.Addresses = optionalStringSlice(obj, "spec", "virtualServerAddress")
		resource.Hostnames = optionalStringSlice(obj, "spec", "host")
		resource.Selector = nestedStringMap(obj, "spec", "selector", "matchLabels")
		copyNestedString(obj, resource.Properties, "partition", "spec", "partition")
		copyNestedString(obj, resource.Properties, "ipamLabel", "spec", "ipamLabel")
	case ExternalKindF5Virtual:
		resource.Addresses = optionalStringSlice(obj, "spec", "virtualServerAddress")
		resource.Hostnames = optionalStringSlice(obj, "spec", "host")
		copyNestedString(obj, resource.Properties, "hostGroup", "spec", "hostGroup")
		resource.Backends = f5VirtualBackends(obj)
	case ExternalKindF5Transport:
		resource.Addresses = optionalStringSlice(obj, "spec", "virtualServerAddress")
		copyNestedInt(obj, resource.Properties, "virtualServerPort", "spec", "virtualServerPort")
		resource.Backends = f5TransportBackends(obj)
	default:
		return ExternalResource{}, false
	}

	if resource.Properties["status"] == "" {
		delete(resource.Properties, "status")
	}
	return resource, true
}

func gatewayAddresses(obj *unstructured.Unstructured) []string {
	items, ok, _ := unstructured.NestedSlice(obj.Object, "status", "addresses")
	if !ok {
		return nil
	}
	var values []string
	for _, item := range items {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if value, ok, _ := unstructured.NestedString(m, "value"); ok && value != "" {
			values = append(values, value)
		}
	}
	return values
}

func listenerHostnames(obj *unstructured.Unstructured) []string {
	items, ok, _ := unstructured.NestedSlice(obj.Object, "spec", "listeners")
	if !ok {
		return nil
	}
	var values []string
	for _, item := range items {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if value, ok, _ := unstructured.NestedString(m, "hostname"); ok && value != "" {
			values = append(values, value)
		}
	}
	return values
}

func gatewayListeners(obj *unstructured.Unstructured) []string {
	items, ok, _ := unstructured.NestedSlice(obj.Object, "spec", "listeners")
	if !ok {
		return nil
	}
	var values []string
	for _, item := range items {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		name, _, _ := unstructured.NestedString(m, "name")
		protocol, _, _ := unstructured.NestedString(m, "protocol")
		port, _, _ := unstructured.NestedInt64(m, "port")
		if name != "" || protocol != "" || port != 0 {
			values = append(values, fmt.Sprintf("%s:%s/%d", name, protocol, port))
		}
	}
	return values
}

func parentRefs(obj *unstructured.Unstructured) []ExternalParentRef {
	items, ok, _ := unstructured.NestedSlice(obj.Object, "spec", "parentRefs")
	if !ok {
		return nil
	}
	var refs []ExternalParentRef
	for _, item := range items {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		name, _, _ := unstructured.NestedString(m, "name")
		if name == "" {
			continue
		}
		group, _, _ := unstructured.NestedString(m, "group")
		kind, _, _ := unstructured.NestedString(m, "kind")
		namespace, _, _ := unstructured.NestedString(m, "namespace")
		sectionName, _, _ := unstructured.NestedString(m, "sectionName")
		refs = append(refs, ExternalParentRef{
			Group:       group,
			Kind:        kind,
			Namespace:   namespace,
			Name:        name,
			SectionName: sectionName,
		})
	}
	return refs
}

func routePaths(obj *unstructured.Unstructured, kind ExternalResourceKind) []string {
	if kind != ExternalKindHTTPRoute {
		return nil
	}
	rules, ok, _ := unstructured.NestedSlice(obj.Object, "spec", "rules")
	if !ok {
		return nil
	}
	paths := make(map[string]struct{})
	for _, rule := range rules {
		ruleMap, ok := rule.(map[string]interface{})
		if !ok {
			continue
		}
		matches, ok, _ := unstructured.NestedSlice(ruleMap, "matches")
		if !ok || len(matches) == 0 {
			paths["/"] = struct{}{}
			continue
		}
		for _, match := range matches {
			matchMap, ok := match.(map[string]interface{})
			if !ok {
				continue
			}
			value, ok, _ := unstructured.NestedString(matchMap, "path", "value")
			if !ok || value == "" {
				value = "/"
			}
			paths[value] = struct{}{}
		}
	}
	values := make([]string, 0, len(paths))
	for path := range paths {
		values = append(values, path)
	}
	sort.Strings(values)
	return values
}

func routeBackendRefs(obj *unstructured.Unstructured) []ExternalBackendRef {
	rules, ok, _ := unstructured.NestedSlice(obj.Object, "spec", "rules")
	if !ok {
		return nil
	}
	var refs []ExternalBackendRef
	for _, rule := range rules {
		ruleMap, ok := rule.(map[string]interface{})
		if !ok {
			continue
		}
		backendRefs, ok, _ := unstructured.NestedSlice(ruleMap, "backendRefs")
		if !ok {
			continue
		}
		refs = append(refs, backendRefsFromSlice(backendRefs)...)
	}
	return refs
}

func backendRefsFromSlice(items []interface{}) []ExternalBackendRef {
	var refs []ExternalBackendRef
	for _, item := range items {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		name, _, _ := unstructured.NestedString(m, "name")
		if name == "" {
			continue
		}
		group, _, _ := unstructured.NestedString(m, "group")
		kind, _, _ := unstructured.NestedString(m, "kind")
		namespace, _, _ := unstructured.NestedString(m, "namespace")
		port, _, _ := unstructured.NestedInt64(m, "port")
		refs = append(refs, ExternalBackendRef{
			Group:     group,
			Kind:      kind,
			Namespace: namespace,
			Name:      name,
			Port:      intString(port),
		})
	}
	return refs
}

func referenceGrantFrom(obj *unstructured.Unstructured) []ExternalGrantFrom {
	items, ok, _ := unstructured.NestedSlice(obj.Object, "spec", "from")
	if !ok {
		return nil
	}
	refs := make([]ExternalGrantFrom, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		group, _, _ := unstructured.NestedString(m, "group")
		kind, _, _ := unstructured.NestedString(m, "kind")
		namespace, _, _ := unstructured.NestedString(m, "namespace")
		if kind == "" || namespace == "" {
			continue
		}
		refs = append(refs, ExternalGrantFrom{Group: group, Kind: kind, Namespace: namespace})
	}
	return refs
}

func referenceGrantTo(obj *unstructured.Unstructured) []ExternalGrantTo {
	items, ok, _ := unstructured.NestedSlice(obj.Object, "spec", "to")
	if !ok {
		return nil
	}
	refs := make([]ExternalGrantTo, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		group, _, _ := unstructured.NestedString(m, "group")
		kind, _, _ := unstructured.NestedString(m, "kind")
		name, _, _ := unstructured.NestedString(m, "name")
		if kind == "" {
			continue
		}
		refs = append(refs, ExternalGrantTo{Group: group, Kind: kind, Name: name})
	}
	return refs
}

func f5VirtualBackends(obj *unstructured.Unstructured) []ExternalBackendRef {
	pools, ok, _ := unstructured.NestedSlice(obj.Object, "spec", "pools")
	if !ok {
		return nil
	}
	var refs []ExternalBackendRef
	for _, pool := range pools {
		m, ok := pool.(map[string]interface{})
		if !ok {
			continue
		}
		service, _, _ := unstructured.NestedString(m, "service")
		if service == "" {
			continue
		}
		servicePort, _, _ := unstructured.NestedInt64(m, "servicePort")
		refs = append(refs, ExternalBackendRef{Name: service, Port: intString(servicePort)})
	}
	return refs
}

func f5TransportBackends(obj *unstructured.Unstructured) []ExternalBackendRef {
	service, _, _ := unstructured.NestedString(obj.Object, "spec", "pool", "service")
	if service == "" {
		return nil
	}
	servicePort, _, _ := unstructured.NestedInt64(obj.Object, "spec", "pool", "servicePort")
	return []ExternalBackendRef{{Name: service, Port: intString(servicePort)}}
}

func copyConditionStatus(obj *unstructured.Unstructured, properties map[string]string) {
	conditions := statusConditions(obj)
	if len(conditions) == 0 {
		return
	}
	for key, condition := range conditions {
		properties[key] = condition.status
	}

	priority := []string{"resolvedRefs", "accepted", "programmed", "ready"}
	for _, key := range priority {
		if condition, ok := conditions[key]; ok && condition.status == "False" {
			properties["status"] = condition.conditionType + "=False"
			if condition.reason != "" {
				properties["statusReason"] = condition.reason
			}
			return
		}
	}
	for i := len(priority) - 1; i >= 0; i-- {
		key := priority[i]
		if condition, ok := conditions[key]; ok && condition.status == "True" {
			properties["status"] = condition.conditionType
			return
		}
	}
}

type externalCondition struct {
	conditionType string
	status        string
	reason        string
}

func statusConditions(obj *unstructured.Unstructured) map[string]externalCondition {
	result := make(map[string]externalCondition)
	appendConditions := func(items []interface{}) {
		for _, item := range items {
			m, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			conditionType, _, _ := unstructured.NestedString(m, "type")
			status, _, _ := unstructured.NestedString(m, "status")
			if conditionType == "" || status == "" {
				continue
			}
			key := lowerCamel(conditionType)
			reason, _, _ := unstructured.NestedString(m, "reason")
			candidate := externalCondition{conditionType: conditionType, status: status, reason: reason}
			current, exists := result[key]
			if !exists || status == "False" || current.status == "Unknown" {
				result[key] = candidate
			}
		}
	}

	if items, ok, _ := unstructured.NestedSlice(obj.Object, "status", "conditions"); ok {
		appendConditions(items)
	}
	for _, collection := range []string{"parents", "listeners"} {
		entries, ok, _ := unstructured.NestedSlice(obj.Object, "status", collection)
		if !ok {
			continue
		}
		for _, entry := range entries {
			m, ok := entry.(map[string]interface{})
			if !ok {
				continue
			}
			if items, ok, _ := unstructured.NestedSlice(m, "conditions"); ok {
				appendConditions(items)
			}
		}
	}

	return result
}

func lowerCamel(value string) string {
	if value == "" {
		return ""
	}
	return strings.ToLower(value[:1]) + value[1:]
}

func stringSlice(obj *unstructured.Unstructured, fields ...string) []string {
	items, ok, _ := unstructured.NestedStringSlice(obj.Object, fields...)
	if !ok {
		return nil
	}
	return items
}

func optionalStringSlice(obj *unstructured.Unstructured, fields ...string) []string {
	value, ok, _ := unstructured.NestedString(obj.Object, fields...)
	if !ok || value == "" {
		return nil
	}
	return []string{value}
}

func nestedStringMap(obj *unstructured.Unstructured, fields ...string) map[string]string {
	values, ok, _ := unstructured.NestedStringMap(obj.Object, fields...)
	if !ok {
		return nil
	}
	return values
}

func copyNestedString(obj *unstructured.Unstructured, target map[string]string, key string, fields ...string) {
	value, ok, _ := unstructured.NestedString(obj.Object, fields...)
	if ok && value != "" {
		target[key] = value
	}
}

func copyNestedInt(obj *unstructured.Unstructured, target map[string]string, key string, fields ...string) {
	value, ok, _ := unstructured.NestedInt64(obj.Object, fields...)
	if ok && value != 0 {
		target[key] = strconv.FormatInt(value, 10)
	}
}

func intString(value int64) string {
	if value == 0 {
		return ""
	}
	return strconv.FormatInt(value, 10)
}
