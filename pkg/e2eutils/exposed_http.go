package e2eutils

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"

	"github.com/phayes/freeport"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"

	tnet "github.com/inercia/kubetnl/pkg/net"
	prt "github.com/inercia/kubetnl/pkg/port"
	"github.com/inercia/kubetnl/pkg/tunnel"
)

// WriteFunc is a function that implements the io.Writer interface.
type WriteFunc func(p []byte) (n int, err error)

func (w WriteFunc) Write(p []byte) (n int, err error) {
	return w(p)
}

// ExposedHTTPServerConfig is the configuration for the exposed service
type ExposedHTTPServerConfig struct {
	// Name is the service/pod/configmap name
	Name string

	// Namespace is the namespace where this service will run
	Namespace string

	// Port is the remote port exposed by the service (ie, 8080)
	// Traffic to this port will be redirected to the local HTTP server
	Port int

	// Config is a REST config
	Config *rest.Config
}

// ExposedHTTPServer is a simple helper classed used for running an HTTP server locally
// but exposing it in a remote kubernetes cluster with the help of a tunnel.
type ExposedHTTPServer struct {
	ExposedHTTPServerConfig

	tun             *tunnel.Tunnel
	httpServer      *httptest.Server
	kubeToHereReady chan struct{}
}

// NewExposedHTTPServer creates a new exposed HTTP server.
func NewExposedHTTPServer(config ExposedHTTPServerConfig) *ExposedHTTPServer {
	return &ExposedHTTPServer{
		ExposedHTTPServerConfig: config,
	}
}

// Run runs a local HTTP server and exposes the service in Kubernetes.
//
// All the traffic that is sent to the exposed service at the given port will be
// redirected and processed by the handler function.
func (e *ExposedHTTPServer) Start(ctx context.Context, handler http.Handler) (chan struct{}, error) {
	e.httpServer = httptest.NewServer(handler)

	klog.Infof("Local HTTP server started at %s", e.httpServer.URL)
	u, err := url.Parse(e.httpServer.URL)
	if err != nil {
		return nil, err
	}

	listenerHost, listenerPortS, _ := net.SplitHostPort(u.Host)
	listenerPort, err := strconv.Atoi(listenerPortS)
	if err != nil {
		return nil, err
	}

	cs, err := kubernetes.NewForConfig(e.Config)
	if err != nil {
		return nil, err
	}

	streams := genericclioptions.IOStreams{In: os.Stdin}
	streams.Out = WriteFunc(func(p []byte) (n int, err error) {
		klog.Infof("%s", p)
		return len(p), nil
	})
	streams.ErrOut = WriteFunc(func(p []byte) (n int, err error) {
		klog.Infof("ERROR: %s", p)
		return len(p), nil
	})

	kubeToHereConfig := tunnel.TunnelConfig{
		Name:             e.Name,
		IOStreams:        streams,
		Image:            tunnel.DefaultTunnelImage,
		Namespace:        e.Namespace,
		EnforceNamespace: true,
		PortMappings: []prt.Mapping{
			{
				TargetIP:            listenerHost,
				TargetPortNumber:    listenerPort,
				ContainerPortNumber: e.Port,
			},
		},
		ContinueOnTunnelError: true,
		RESTConfig:            e.Config,
		ClientSet:             cs,
	}

	kubeToHereConfig.LocalSSHPort, err = freeport.GetFreePort()
	if err != nil {
		return nil, err
	}

	kubeToHereConfig.RemoteSSHPort, err = tnet.GetFreeSSHPortInContainer(kubeToHereConfig.PortMappings)
	if err != nil {
		return nil, err
	}

	klog.Infof("Creating a tunnel kubernetes[%s:%d]->here:%d",
		kubeToHereConfig.Name,
		kubeToHereConfig.PortMappings[0].ContainerPortNumber,
		kubeToHereConfig.PortMappings[0].TargetPortNumber)

	e.tun = tunnel.NewTunnel(kubeToHereConfig)

	klog.Infof("Starting kube->here tunnel...")
	e.kubeToHereReady, err = e.tun.Run(ctx)
	if err != nil {
		return nil, err
	}

	return e.kubeToHereReady, nil
}

func (e *ExposedHTTPServer) Ready() <-chan struct{} {
	return e.kubeToHereReady
}

func (e *ExposedHTTPServer) Stop() error {
	if e.tun != nil {
		klog.Infof("Stopping tunnel kubernetes[%s:%d]->%s...", e.Name, e.Port, e.httpServer.Listener.Addr())
		_ = e.tun.Stop(context.Background())
	}

	if e.httpServer != nil {
		klog.V(3).Infof("Stopping HTTP server...")
		e.httpServer.Close()
	}

	return nil
}
