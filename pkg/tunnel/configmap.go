package tunnel

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

const (
	scriptFilename = "ssh-init.sh"
	scriptContents = `
#!/bin/bash
# set -e
if [[ ! -z "${PORT}" ]]; then
  echo "Port ${PORT}\n" >> /etc/ssh/sshd_config
fi

sed -i 's/#AllowAgentForwarding yes/AllowAgentForwarding yes/g' /etc/ssh/sshd_config
sed -i 's/AllowTcpForwarding no/AllowTcpForwarding yes/g' /etc/ssh/sshd_config
sed -i 's/GatewayPorts no/GatewayPorts yes/g' /etc/ssh/sshd_config
sed -i 's/X11Forwarding no/X11Forwarding yes/g' /etc/ssh/sshd_config
`
	scriptDirectory = "/custom-cont-init.d"
)

func getConfigMap(name string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"io.github.kubetnl": name,
			},
		},
		Data: map[string]string{
			"ssh-init.sh": scriptContents,
		},
	}
}

func (o *Tunnel) CreateConfigMap(ctx context.Context) error {
	var err error

	o.configMapClient = o.ClientSet.CoreV1().ConfigMaps(o.Namespace)
	o.configMap = getConfigMap(o.Name)

	klog.V(3).Infof("Creating ConfigMap %q...", o.Name)
	o.configMap, err = o.configMapClient.Create(ctx, o.configMap, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("error creating configMap: %v", err)
	}

	klog.V(3).Infof("Created ConfigMap %q.", o.configMap.GetObjectMeta().GetName())
	return nil
}

func (o *Tunnel) CleanupConfigMap(ctx context.Context) error {
	deletePolicy := metav1.DeletePropagationForeground
	deleteOptions := metav1.DeleteOptions{PropagationPolicy: &deletePolicy}

	klog.V(2).Infof("Cleanup: deleting config map %s ...", o.configMap.Name)
	if err := o.configMapClient.Delete(ctx, o.configMap.Name, deleteOptions); err != nil {
		klog.V(1).Infof("Cleanup: error deleting config map: %v. That configMap probably still runs. You can use kubetnl cleanup to clean up all resources created by kubetnl.", err)
		fmt.Fprintf(o.ErrOut, "Failed to delete config map %q. Use \"kubetnl cleanup\" to delete any leftover resources created by kubetnl.\n", o.Name)
	}

	return nil
}
