package tunnel

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/klog/v2"

	"github.com/inercia/kubetnl/pkg/port"
)

func getService(name string, ports []corev1.ServicePort) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"io.github.kubetnl": name,
			},
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"io.github.kubetnl": name,
			},
			Ports: ports,
		},
	}
}

// CreateService creates the `Service` that will listen at the list of port mappings
// and send that traffic to the `Pod`.
func (o *Tunnel) CreateService(ctx context.Context) error {
	var err error

	// Create the service for incoming traffic within the cluster. The
	// services accepts traffic on all ports that are in mentioned in
	// o.PortMappings[*].ContainerPortNumber using the specied protocol.
	o.serviceClient = o.ClientSet.CoreV1().Services(o.Namespace)

	svcPorts := servicePorts(o.PortMappings)
	o.service = getService(o.Name, svcPorts)

	klog.V(3).Infof("Creating Service %q...", o.Name)
	o.service, err = o.serviceClient.Create(ctx, o.service, metav1.CreateOptions{})
	if err != nil {
		if errors.IsAlreadyExists(err) {
			klog.V(3).Infof("error creating Service: %v (IGNORED)", err)
		} else {
			return fmt.Errorf("error creating Service: %v", err)
		}
	}

	klog.V(3).Infof("Created Service %q.", o.service.GetObjectMeta().GetName())
	return nil
}

func (o *Tunnel) CleanupService(ctx context.Context) error {
	deletePolicy := metav1.DeletePropagationForeground
	deleteOptions := metav1.DeleteOptions{PropagationPolicy: &deletePolicy}

	if o.service != nil {
		klog.V(2).Infof("Cleanup: deleting Service %s ...", o.service.Name)
		err := o.serviceClient.Delete(ctx, o.service.Name, deleteOptions)
		if err != nil {
			klog.V(1).Info("Cleanup: error deleting Service: %v", err)
			fmt.Fprintf(o.ErrOut, "Failed to delete service %q. Use \"kubetnl cleanup\" to delete any leftover resources created by kubetnl.\n", o.Name)
		}
	}

	return nil
}

func servicePorts(mappings []port.Mapping) []corev1.ServicePort {
	var ports []corev1.ServicePort
	for i, m := range mappings {
		ports = append(ports, corev1.ServicePort{
			Name:       fmt.Sprint(i),
			Port:       int32(m.ContainerPortNumber),
			TargetPort: intstr.FromInt(m.ContainerPortNumber),
			Protocol:   protocolToCoreV1(m.Protocol),
		})
	}
	return ports
}
