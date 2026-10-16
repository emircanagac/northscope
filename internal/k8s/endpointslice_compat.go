package k8s

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	discoveryv1beta1 "k8s.io/api/discovery/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"
)

const endpointSliceResource = "endpointslices"

var (
	endpointSliceV1      = discoveryv1.SchemeGroupVersion.String()
	endpointSliceV1beta1 = discoveryv1beta1.SchemeGroupVersion.String()
)

// endpointSliceSource reads EndpointSlices from the discovery.k8s.io version
// served by the cluster. Kubernetes 1.20 only serves v1beta1; 1.21+ serves v1.
// The builder always consumes discovery/v1 objects.
type endpointSliceSource struct {
	apiVersion string
	informer   cache.SharedIndexInformer
	list       func() ([]*discoveryv1.EndpointSlice, error)
}

// detectEndpointSliceAPI returns the preferred served EndpointSlice
// group/version, or an empty string when the cluster serves neither version.
func detectEndpointSliceAPI(client discovery.DiscoveryInterface) (string, error) {
	if client == nil {
		return endpointSliceV1, nil
	}
	for _, groupVersion := range []string{endpointSliceV1, endpointSliceV1beta1} {
		resources, err := client.ServerResourcesForGroupVersion(groupVersion)
		if err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return "", fmt.Errorf("discover %s: %w", groupVersion, err)
		}
		for _, resource := range resources.APIResources {
			if resource.Name == endpointSliceResource {
				return groupVersion, nil
			}
		}
	}
	return "", nil
}

func newEndpointSliceSource(factory informers.SharedInformerFactory, apiVersion string) *endpointSliceSource {
	switch apiVersion {
	case endpointSliceV1:
		informer := factory.Discovery().V1().EndpointSlices()
		return &endpointSliceSource{
			apiVersion: apiVersion,
			informer:   informer.Informer(),
			list: func() ([]*discoveryv1.EndpointSlice, error) {
				return informer.Lister().List(labels.Everything())
			},
		}
	case endpointSliceV1beta1:
		informer := factory.Discovery().V1beta1().EndpointSlices()
		return &endpointSliceSource{
			apiVersion: apiVersion,
			informer:   informer.Informer(),
			list: func() ([]*discoveryv1.EndpointSlice, error) {
				items, err := informer.Lister().List(labels.Everything())
				if err != nil {
					return nil, err
				}
				converted := make([]*discoveryv1.EndpointSlice, 0, len(items))
				for _, item := range items {
					converted = append(converted, convertV1beta1EndpointSlice(item))
				}
				return converted, nil
			},
		}
	default:
		return nil
	}
}

// convertV1beta1EndpointSlice mirrors the API server's v1beta1 -> v1
// conversion for the fields NorthScope reads.
func convertV1beta1EndpointSlice(in *discoveryv1beta1.EndpointSlice) *discoveryv1.EndpointSlice {
	out := &discoveryv1.EndpointSlice{
		TypeMeta:    in.TypeMeta,
		ObjectMeta:  *in.ObjectMeta.DeepCopy(),
		AddressType: discoveryv1.AddressType(in.AddressType),
	}
	out.APIVersion = endpointSliceV1

	if len(in.Endpoints) > 0 {
		out.Endpoints = make([]discoveryv1.Endpoint, 0, len(in.Endpoints))
	}
	for _, endpoint := range in.Endpoints {
		converted := discoveryv1.Endpoint{
			Addresses: append([]string(nil), endpoint.Addresses...),
			Conditions: discoveryv1.EndpointConditions{
				Ready:       endpoint.Conditions.Ready,
				Serving:     endpoint.Conditions.Serving,
				Terminating: endpoint.Conditions.Terminating,
			},
			Hostname: endpoint.Hostname,
			NodeName: endpoint.NodeName,
		}
		if endpoint.TargetRef != nil {
			converted.TargetRef = endpoint.TargetRef.DeepCopy()
		}
		if len(endpoint.Topology) > 0 {
			topology := make(map[string]string, len(endpoint.Topology))
			for key, value := range endpoint.Topology {
				topology[key] = value
			}
			if zone, ok := topology[corev1.LabelTopologyZone]; ok {
				converted.Zone = &zone
				delete(topology, corev1.LabelTopologyZone)
			}
			// Kubernetes 1.20 records the Node only in topology unless the
			// alpha EndpointSliceNodeName gate is enabled.
			if hostname, ok := topology[corev1.LabelHostname]; ok {
				if converted.NodeName == nil {
					converted.NodeName = &hostname
				}
				if *converted.NodeName == hostname {
					delete(topology, corev1.LabelHostname)
				}
			}
			if len(topology) > 0 {
				converted.DeprecatedTopology = topology
			}
		}
		out.Endpoints = append(out.Endpoints, converted)
	}

	if len(in.Ports) > 0 {
		out.Ports = make([]discoveryv1.EndpointPort, 0, len(in.Ports))
	}
	for _, port := range in.Ports {
		out.Ports = append(out.Ports, discoveryv1.EndpointPort{
			Name:        port.Name,
			Protocol:    port.Protocol,
			Port:        port.Port,
			AppProtocol: port.AppProtocol,
		})
	}
	return out
}
