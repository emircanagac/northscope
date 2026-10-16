package k8s

type ExternalResourceKind string

const (
	ExternalKindGatewayClass   ExternalResourceKind = "GatewayClass"
	ExternalKindGateway        ExternalResourceKind = "Gateway"
	ExternalKindHTTPRoute      ExternalResourceKind = "HTTPRoute"
	ExternalKindGRPCRoute      ExternalResourceKind = "GRPCRoute"
	ExternalKindTLSRoute       ExternalResourceKind = "TLSRoute"
	ExternalKindTCPRoute       ExternalResourceKind = "TCPRoute"
	ExternalKindUDPRoute       ExternalResourceKind = "UDPRoute"
	ExternalKindReferenceGrant ExternalResourceKind = "ReferenceGrant"
	ExternalKindF5IngressLink  ExternalResourceKind = "F5IngressLink"
	ExternalKindF5Virtual      ExternalResourceKind = "F5VirtualServer"
	ExternalKindF5Transport    ExternalResourceKind = "F5TransportServer"
)

type ExternalResource struct {
	Kind       ExternalResourceKind
	APIVersion string
	Namespace  string
	Name       string

	ClassName  string
	Addresses  []string
	Hostnames  []string
	Paths      []string
	Listeners  []string
	ParentRefs []ExternalParentRef
	Backends   []ExternalBackendRef
	GrantFrom  []ExternalGrantFrom
	GrantTo    []ExternalGrantTo
	Selector   map[string]string

	Properties map[string]string
}

type ExternalParentRef struct {
	Group       string
	Kind        string
	Namespace   string
	Name        string
	SectionName string
}

type ExternalBackendRef struct {
	Group     string
	Kind      string
	Namespace string
	Name      string
	Port      string
}

type ExternalGrantFrom struct {
	Group     string
	Kind      string
	Namespace string
}

type ExternalGrantTo struct {
	Group string
	Kind  string
	Name  string
}
