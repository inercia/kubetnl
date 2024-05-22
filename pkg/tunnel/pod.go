package tunnel

import (
	"context"
	"fmt"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	watchtools "k8s.io/client-go/tools/watch"
	"k8s.io/klog/v2"

	"github.com/inercia/kubetnl/pkg/graceful"
	"github.com/inercia/kubetnl/pkg/port"
)

var kubetnlPodContainerName = "main"

func getServiceAccount(name string) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"io.github.kubetnl": name,
			},
		},
	}
}

func getPod(name, image string, sshPort int, ports []corev1.ContainerPort) *corev1.Pod {
	ptrInt32 := func(i int32) *int32 { return &i }

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"io.github.kubetnl": name,
			},
		},
		Spec: corev1.PodSpec{
			ServiceAccountName: string(name),
			Containers: []corev1.Container{{
				Name:            kubetnlPodContainerName,
				Image:           image,
				ImagePullPolicy: corev1.PullPolicy(corev1.PullIfNotPresent),
				Ports:           ports,
				Env: []corev1.EnvVar{
					{Name: "PORT", Value: strconv.Itoa(sshPort)},
					{Name: "PASSWORD_ACCESS", Value: "true"},
					{Name: "USER_NAME", Value: "user"},
					{Name: "USER_PASSWORD", Value: "password"},
				},
				VolumeMounts: []corev1.VolumeMount{{
					Name:      "scripts",
					MountPath: scriptDirectory,
				}},
				ReadinessProbe: &corev1.Probe{
					ProbeHandler: corev1.ProbeHandler{
						TCPSocket: &corev1.TCPSocketAction{
							Port: intstr.FromInt(sshPort),
						},
					},
					InitialDelaySeconds: 5,
					PeriodSeconds:       5,
					FailureThreshold:    3,
				},
			}},
			Volumes: []corev1.Volume{{
				Name: "scripts",
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: name,
						},
						Items: []corev1.KeyToPath{
							{
								Key:  scriptFilename,
								Path: scriptFilename,
								Mode: ptrInt32(0o755),
							},
						},
					},
				},
			}},
		},
	}
}

func (o *Tunnel) CreatePod(ctx context.Context) error {
	var err error

	// Create the service for incoming traffic within the cluster. The pod
	// exposes all ports that are in mentioned in
	// o.PortMappings[*].ContainerPortNumber using the specied protocol.
	// Additionally it exposes the port for the ssh conn.
	ports := append(containerPorts(o.PortMappings), corev1.ContainerPort{
		Name:          "ssh",
		ContainerPort: int32(o.RemoteSSHPort),
	})

	o.serviceAccountClient = o.ClientSet.CoreV1().ServiceAccounts(o.Namespace)
	o.serviceAccount = getServiceAccount(o.Name)

	klog.V(2).Infof("Creating ServiceAccount %q...", o.Name)
	o.serviceAccount, err = o.serviceAccountClient.Create(ctx, o.serviceAccount, metav1.CreateOptions{})
	if err != nil {
		if !errors.IsAlreadyExists(err) {
			return fmt.Errorf("error creating ServiceAccount %q: %v", o.serviceAccount.Name, err)
		}
	}

	o.podClient = o.ClientSet.CoreV1().Pods(o.Namespace)
	o.pod = getPod(o.Name, o.Image, o.RemoteSSHPort, ports)

	klog.V(2).Infof("Creating Pod %q...", o.Name)
	o.pod, err = o.podClient.Create(ctx, o.pod, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("error creating Pod: %v", err)
	}

	klog.V(3).Infof("Created Pod %q.", o.service.GetObjectMeta().GetName())

	klog.V(3).Infof("Waiting for the Pod to be ready before setting up a SSH connection.")
	watchOptions := metav1.ListOptions{}
	watchOptions.FieldSelector = fields.OneTermEqualSelector("metadata.name", o.Name).String()
	watchOptions.ResourceVersion = o.pod.GetResourceVersion()
	podWatch, err := o.podClient.Watch(ctx, watchOptions)
	if err != nil {
		return fmt.Errorf("error watching Pod %s: %v", o.Name, err)
	}

	_, err = watchtools.UntilWithoutRetry(ctx, podWatch, condPodReady)
	if err != nil {
		if err == watchtools.ErrWatchClosed {
			return fmt.Errorf("error waiting for Pod ready: podWatch has been closed before pod ready event received")
		}

		// err will be wait.ErrWatchClosed is the context passed to
		// watchtools.UntilWithoutRetry is done. However, if the interrupt
		// context was canceled, return an graceful.Interrupted.
		if ctx.Err() != nil {
			return graceful.Interrupted
		}
		if err == wait.ErrWaitTimeout {
			return fmt.Errorf("error waiting for Pod ready: timed out after %d seconds", 300)
		}
		return fmt.Errorf("error waiting for Pod ready: received unknown error \"%f\"", err)
	}

	klog.V(2).Infof("Pod ready...")
	return nil
}

func (o *Tunnel) CleanupPod(ctx context.Context) error {
	deletePolicy := metav1.DeletePropagationForeground
	deleteOptions := metav1.DeleteOptions{PropagationPolicy: &deletePolicy}

	if o.pod != nil {
		klog.V(2).Infof("Cleanup: deleting pod %s ...", o.pod.Name)
		if err := o.podClient.Delete(ctx, o.pod.Name, deleteOptions); err != nil {
			klog.V(1).Infof("Cleanup: error deleting Pod: %v. That pod probably still runs. You can use kubetnl cleanup to clean up all resources created by kubetnl.", err)
			fmt.Fprintf(o.ErrOut, "Failed to delete Pod %q. Use \"kubetnl cleanup\" to delete any leftover resources created by kubetnl.\n", o.Name)
		}
	}

	if o.serviceAccount != nil {
		klog.V(2).Infof("Cleanup: deleting service account %s ...", o.serviceAccount.Name)
		if err := o.serviceAccountClient.Delete(ctx, o.serviceAccount.Name, deleteOptions); err != nil {
			klog.V(1).Infof("Cleanup: error deleting ServiceAccount : %v. You can use kubetnl cleanup to clean up all resources created by kubetnl.", err)
			fmt.Fprintf(o.ErrOut, "Failed to delete ServiceAccount %q. Use \"kubetnl cleanup\" to delete any leftover resources created by kubetnl.\n", o.serviceAccount.Name)
		}
	}

	return nil
}

func containerPorts(mappings []port.Mapping) []corev1.ContainerPort {
	var ports []corev1.ContainerPort
	for _, m := range mappings {
		ports = append(ports, corev1.ContainerPort{
			ContainerPort: int32(m.ContainerPortNumber),
			Protocol:      protocolToCoreV1(m.Protocol),
			// TODO: HostIP?
		})
	}
	return ports
}

func condPodReady(event watch.Event) (bool, error) {
	pod := event.Object.(*corev1.Pod)
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
			klog.V(3).Infof("Tunnel pod check: it is ready !!")
			return true, nil
		}
	}

	klog.V(3).Infof("Tunnel pod check: it is NOT ready yet.")
	return false, nil
}
