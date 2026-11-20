// Command demo runs NorthScope against a generated in-memory cluster, for UI
// development, screenshots, and trying large topologies without a cluster.
//
//	go run ./hack/demo                      # API and built UI on :8080
//	go run ./hack/demo -pods-per-service 20 # a larger cluster
//	cd ui && npm run dev                    # live-reloading UI on :5173
//
// Every few seconds a Pod changes readiness, so the UI receives live deltas.
// A few routes are broken on purpose so their diagnostics show.
package main

import (
	"context"
	"errors"
	"flag"
	"io/fs"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	fakediscovery "k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/emircanagac/northscope/internal/k8s"
	"github.com/emircanagac/northscope/internal/k8s/synthetic"
	"github.com/emircanagac/northscope/internal/server"
	"github.com/emircanagac/northscope/ui"
)

func main() {
	var (
		addr       = flag.String("addr", "127.0.0.1:8080", "HTTP listen address")
		namespaces = flag.Int("namespaces", 8, "Application namespaces")
		services   = flag.Int("services-per-namespace", 4, "Services, and hosts, per namespace")
		pods       = flag.Int("pods-per-service", 3, "Pods per Service")
		nodes      = flag.Int("nodes", 6, "Nodes")
		churn      = flag.Duration("churn", 4*time.Second, "How often a Pod changes readiness; 0 disables live changes")
	)
	flag.Parse()

	cluster := synthetic.New(synthetic.Spec{
		Namespaces:           *namespaces,
		ServicesPerNamespace: *services,
		PodsPerService:       *pods,
		Nodes:                *nodes,
	})
	breakSomeRoutes(&cluster)
	client := fake.NewSimpleClientset(cluster.Objects()...)
	// The fake discovery serves no APIs by default; serve EndpointSlices like
	// any cluster since Kubernetes 1.21 so the generated slices are read.
	client.Discovery().(*fakediscovery.FakeDiscovery).Resources = []*metav1.APIResourceList{{
		GroupVersion: "discovery.k8s.io/v1",
		APIResources: []metav1.APIResource{{Name: "endpointslices", Kind: "EndpointSlice", Namespaced: true}},
	}}

	watcher, err := k8s.NewWatcherFromClient(client, time.Minute)
	if err != nil {
		fatal("Create watcher failed", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		if err := watcher.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("Watcher stopped", "error", err)
		}
	}()
	if *churn > 0 {
		go flipPodReadiness(ctx, client, cluster.Pods, *churn)
	}

	staticFS, err := fs.Sub(ui.Dist, "dist")
	if err != nil {
		fatal("Load embedded UI failed", err)
	}
	httpServer := server.New(*addr, watcher, staticFS, "demo")
	go func() {
		slog.Info("Demo listening", "addr", *addr, "pods", len(cluster.Pods), "services", len(cluster.Services))
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fatal("HTTP server failed", err)
		}
	}()
	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(shutdown)
}

// breakSomeRoutes gives the demo the problems NorthScope diagnoses: a backend
// whose Pods crash, one with no Pods, and a path to a missing Service.
func breakSomeRoutes(cluster *synthetic.Cluster) {
	for _, pod := range cluster.Pods {
		switch {
		case pod.Namespace == "team-000" && pod.Labels["app"] == "svc-01":
			pod.Status.Conditions[1].Status = corev1.ConditionFalse
			pod.Status.ContainerStatuses[0].Ready = false
			pod.Status.ContainerStatuses[0].State = corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}}
		case pod.Namespace == "team-001" && pod.Labels["app"] == "svc-02":
			pod.Labels = map[string]string{"app": "svc-02-old"}
		}
	}
	for _, slice := range cluster.EndpointSlices {
		if slice.Namespace == "team-000" && slice.Labels["kubernetes.io/service-name"] == "svc-01" {
			notReady := false
			for i := range slice.Endpoints {
				slice.Endpoints[i].Conditions.Ready = &notReady
			}
		}
		if slice.Namespace == "team-001" && slice.Labels["kubernetes.io/service-name"] == "svc-02" {
			slice.Endpoints = nil
		}
	}
	if len(cluster.Ingresses) > 2 {
		pathType := networkingv1.PathTypePrefix
		ingress := cluster.Ingresses[2]
		ingress.Spec.Rules[0].HTTP.Paths = append(ingress.Spec.Rules[0].HTTP.Paths, networkingv1.HTTPIngressPath{
			Path:     "/legacy",
			PathType: &pathType,
			Backend: networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{
				Name: "legacy-api",
				Port: networkingv1.ServiceBackendPort{Number: 8080},
			}},
		})
	}
}

func flipPodReadiness(ctx context.Context, client kubernetes.Interface, pods []*corev1.Pod, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		original := pods[rand.IntN(len(pods))]
		pod, err := client.CoreV1().Pods(original.Namespace).Get(ctx, original.Name, metav1.GetOptions{})
		if err != nil {
			continue
		}
		ready := false
		for i := range pod.Status.Conditions {
			if pod.Status.Conditions[i].Type == corev1.PodReady {
				ready = pod.Status.Conditions[i].Status != corev1.ConditionTrue
				pod.Status.Conditions[i].Status = corev1.ConditionFalse
				if ready {
					pod.Status.Conditions[i].Status = corev1.ConditionTrue
				}
			}
		}
		if _, err := client.CoreV1().Pods(pod.Namespace).UpdateStatus(ctx, pod, metav1.UpdateOptions{}); err != nil {
			continue
		}
		setEndpointReady(ctx, client, pod, ready)
	}
}

// setEndpointReady mirrors the EndpointSlice controller: the Pod's endpoint
// follows its readiness.
func setEndpointReady(ctx context.Context, client kubernetes.Interface, pod *corev1.Pod, ready bool) {
	slices, err := client.DiscoveryV1().EndpointSlices(pod.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: discoveryv1.LabelServiceName + "=" + pod.Labels["app"],
	})
	if err != nil {
		return
	}
	for i := range slices.Items {
		slice := &slices.Items[i]
		for j := range slice.Endpoints {
			if ref := slice.Endpoints[j].TargetRef; ref != nil && ref.Name == pod.Name {
				slice.Endpoints[j].Conditions.Ready = &ready
				_, _ = client.DiscoveryV1().EndpointSlices(pod.Namespace).Update(ctx, slice, metav1.UpdateOptions{})
			}
		}
	}
}

func fatal(msg string, err error) {
	slog.Error(msg, "error", err)
	os.Exit(1)
}
