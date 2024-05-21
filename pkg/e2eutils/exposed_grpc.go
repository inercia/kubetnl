package e2eutils

import (
	"context"
	"net"
	"os"
	"strconv"

	"github.com/phayes/freeport"
	"google.golang.org/grpc"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"

	tnet "github.com/inercia/kubetnl/pkg/net"
	prt "github.com/inercia/kubetnl/pkg/port"
	"github.com/inercia/kubetnl/pkg/tunnel"
)

// ExposedGRPCServerConfig is the configuration for the exposed service
type ExposedGRPCServerConfig struct {
	// Name is the service/pod/configmap name
	Name string

	// Namespace is the namespace where this service will run
	Namespace string

	// Port is the remote port exposed by the service (ie, 8080)
	// Traffic to this port will be redirected to the local GRPC server
	Port int

	// Config is a REST config
	Config *rest.Config
}

// ExposedGRPCServer is a simple helper classed used for running an GRPC server locally
// but exposing it in a remote kubernetes cluster with the help of a tunnel.
type ExposedGRPCServer struct {
	ExposedGRPCServerConfig

	tun             *tunnel.Tunnel
	grpcServer      *grpc.Server
	kubeToHereReady chan struct{}
}

// NewExposedGRPCServer creates a new exposed GRPC server.
func NewExposedGRPCServer(config ExposedGRPCServerConfig) *ExposedGRPCServer {
	return &ExposedGRPCServer{
		ExposedGRPCServerConfig: config,
	}
}

// Run runs a local GRPC server and exposes the service in Kubernetes.
//
// All the traffic that is sent to the exposed service at the given port will be
// redirected and processed by the handler function.
func (e *ExposedGRPCServer) Start(ctx context.Context, server *grpc.Server, lis net.Listener) (chan struct{}, error) {
	e.grpcServer = server

	ctx, cancel := context.WithCancel(ctx)

	cs, err := kubernetes.NewForConfig(e.Config)
	if err != nil {
		return nil, err
	}

	go func() {
		e.grpcServer.Serve(lis)
		cancel()
	}()
	go func() {
		// Wait for the context to be done, and then stop the server
		<-ctx.Done()
		e.grpcServer.GracefulStop()
	}()

	addr := lis.Addr()
	listenerHost, listenerPortS, _ := net.SplitHostPort(addr.String())
	listenerPort, err := strconv.Atoi(listenerPortS)
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

func (e *ExposedGRPCServer) Ready() <-chan struct{} {
	return e.kubeToHereReady
}

func (e *ExposedGRPCServer) Stop() error {
	if e.tun != nil {
		klog.Infof("Stopping tunnel kubernetes[%s:%d]->local...", e.Name, e.Port)
		_ = e.tun.Stop(context.Background())
	}

	if e.grpcServer != nil {
		klog.V(3).Infof("Stopping GRPC server...")
		e.grpcServer.GracefulStop()
	}

	return nil
}
