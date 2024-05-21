package test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"testing"

	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"

	"github.com/inercia/kubetnl/pkg/e2eutils"
	"github.com/inercia/kubetnl/pkg/portforward"
)

// WriteFunc is a function that implements the io.Writer interface.
type WriteFunc func(p []byte) (n int, err error)

func (w WriteFunc) Write(p []byte) (n int, err error) {
	return w(p)
}

// Test that exposes a local service on the kubernetes cluster.
//
// This test creates a local web server and "exposes" this service in a kubernetes cluster with the help
// of a tunnel that forwards the traffic from the kubernetes cluster to the local web server
//
// On the other hand, another tunnel is created for sending HTTP requests to the service in Kubernetes.
//
// In summary, we send HTTP requests to a kubernetes service that sends the request back to us...
//
//	┌────────────────────────────────────────────────────────────┐
//	│             ┌────────────────────────────┐                 │
//	│             │       ┌──────────────────┐ │                 │
//	│        ┌────► :8080 ├──────────────────┼─┼──────────┐      │
//	│        │    │ :2222 │                  │ │          │      │
//	│        │    │       └──────────────────┘ │          │      │
//	│        │    └────────────────────────────┘          │      │
//	│        │                       Kubernetes cluster   │      │
//	└────────┼────────────────────────────────────────────┼──────┘
//	      tunnel                                        tunnel
//	┌────────┼────────────────────────────────────────────┼──────┐
//	│   ┌────┼────────────────────────────────────────────┼───┐  │
//	│   │  ┌─┴────────────┐  ┌────────────┐  ┌────────────▼─┐ │  │
//	│   │  │              │  │            │  │       :60190 │ │  │
//	│   │  │ HTTP         │  │ Test       │  │              │ │  │
//	│   │  │ Request      │  │ Machinery  │  │              │ │  │
//	│   │  │ Generator    │  │            │  │ Local Web    │ │  │
//	│   │  │              │  │            │  │ Server       │ │  │
//	│   │  └──────────────┘  └────────────┘  └──────────────┘ │  │
//	│   │                   Test Runner                       │  │
//	│   └─────────────────────────────────────────────────────┘  │
//	│                                             Localhost      │
//	└────────────────────────────────────────────────────────────┘
func TestServiceInCluster(t *testing.T) {
	exposeLocalService := features.New("expose local service").
		Assess("expose local service", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			var err error

			origContext := ctx
			ctx, cancelContext := context.WithCancel(ctx)

			requestReceived := make(chan struct{}, 1)

			klog.Info("Creating a local HTTP server...")
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				klog.Infof("Received a HTTP request: %q: %+v", r.URL.String(), r)

				w.WriteHeader(http.StatusOK)
				w.Write([]byte("Hello, World!"))

				requestReceived <- struct{}{}

				klog.Info("Good! Request received in the local HTTP server.")
			})

			config := cfg.Client().RESTConfig()
			cs, err := kubernetes.NewForConfig(config)
			if err != nil {
				t.Fatal(err)
			}

			kubeToHere := e2eutils.NewExposedHTTPServer(e2eutils.ExposedHTTPServerConfig{
				Name:      "kube-8080",
				Namespace: cfg.Namespace(),
				Port:      8080,
				Config:    config,
			})

			hereToKube, err := portforward.NewKubeForwarder(portforward.KubeForwarderConfig{
				PodName:      kubeToHere.Name,
				PodNamespace: cfg.Namespace(),
				RemotePort:   8080,
				RESTConfig:   config,
				ClientSet:    cs,
			})
			if err != nil {
				t.Fatal(err)
			}

			klog.Infof("Creating a tunnel from here:%d->kubernetes[%s:%d]",
				hereToKube.LocalPort,
				hereToKube.PodName,
				8080)

			klog.Info("Starting both the kube->here and here->kube tunnels")

			klog.Infof("Starting kube->here tunnel...")
			if _, err := kubeToHere.Start(ctx, handler); err != nil {
				t.Fatal(err)
			}
			defer kubeToHere.Stop()

			klog.Infof("Starting here->kube tunnel...")
			if _, err = hereToKube.Run(ctx); err != nil {
				t.Fatal(err)
			}
			defer hereToKube.Stop()

			klog.Infof("Waiting until everything is ready for starting tests...")
			<-kubeToHere.Ready()
			<-hereToKube.Ready()

			klog.Infof("Everything ready: starting tests")
			addr := fmt.Sprintf("http://127.0.0.1:%d/", hereToKube.LocalPort)
			klog.Infof("Checking that we can send a HTTP request to %q", addr)
			response, _ := http.Get(addr)
			if response != nil {
				if response.StatusCode != http.StatusOK {
					t.Fatalf("Response Status code was %d", response.StatusCode)
				}
				b, err := io.ReadAll(response.Body)
				if err != nil {
					t.Fatal(err)
				}
				klog.Infof("Good! Received a response to out request: %s", string(b))
			} else {
				t.Fatalf("No response received")
			}

			select {
			case <-requestReceived:
				klog.Info("Test passed SUCCESSFULLY: received the request and the reponse.")
				cancelContext()
			case <-hereToKube.Done():
				cancelContext()
			case <-ctx.Done():
				t.Fatal("Test FAILED: context was canceled")
			}

			return origContext
		}).Feature()

	// test feature
	testenv.Test(t, exposeLocalService)
}
