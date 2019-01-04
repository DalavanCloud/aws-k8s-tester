package kubernetes

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/aws/aws-k8s-tester/ec2config"
	"github.com/aws/aws-k8s-tester/internal/ssh"
	"github.com/aws/aws-k8s-tester/kubernetesconfig"
	"go.uber.org/zap"
)

func sendKubeAPIServerPKI(
	lg *zap.Logger,
	ec2Config ec2config.Config,
	target ec2config.Instance,
	privateKeyPath string,
	publicKeyPath string,
	rootCAPath string,
	kubeAPIServerConfig kubernetesconfig.KubeAPIServer,
) (err error) {
	var ss ssh.SSH
	ss, err = ssh.New(ssh.Config{
		Logger:        lg,
		KeyPath:       ec2Config.KeyPath,
		PublicIP:      target.PublicIP,
		PublicDNSName: target.PublicDNSName,
		UserName:      ec2Config.UserName,
	})
	if err != nil {
		return fmt.Errorf("failed to create a SSH to %q(%q) (error %v)", ec2Config.ClusterName, target.InstanceID, err)
	}
	if err = ss.Connect(); err != nil {
		return fmt.Errorf("failed to connect to %q(%q) (error %v)", ec2Config.ClusterName, target.InstanceID, err)
	}
	defer ss.Close()

	remotePath := fmt.Sprintf("/home/%s/kube-apiserver.private.key", ec2Config.UserName)
	_, err = ss.Send(
		privateKeyPath,
		remotePath,
		ssh.WithTimeout(15*time.Second),
		ssh.WithRetry(3, 3*time.Second),
	)
	if err != nil {
		return fmt.Errorf("failed to send %q to %q for %q(%q) (error %v)", privateKeyPath, remotePath, ec2Config.ClusterName, target.InstanceID, err)
	}
	copyCmd := fmt.Sprintf("sudo mkdir -p %s && sudo cp %s %s", filepath.Dir(kubeAPIServerConfig.KubeletClientKey), remotePath, kubeAPIServerConfig.KubeletClientKey)
	_, err = ss.Run(
		copyCmd,
		ssh.WithTimeout(15*time.Second),
		ssh.WithRetry(3, 3*time.Second),
	)
	if err != nil {
		return fmt.Errorf("failed to %q for %q(%q) (error %v)", copyCmd, ec2Config.ClusterName, target.InstanceID, err)
	}
	// TODO: use different cert chain?
	copyCmd = fmt.Sprintf("sudo mkdir -p %s && sudo cp %s %s", filepath.Dir(kubeAPIServerConfig.TLSPrivateKeyFile), remotePath, kubeAPIServerConfig.TLSPrivateKeyFile)
	_, err = ss.Run(
		copyCmd,
		ssh.WithTimeout(15*time.Second),
		ssh.WithRetry(3, 3*time.Second),
	)
	if err != nil {
		return fmt.Errorf("failed to %q for %q(%q) (error %v)", copyCmd, ec2Config.ClusterName, target.InstanceID, err)
	}

	remotePath = fmt.Sprintf("/home/%s/kube-apiserver.public.pem", ec2Config.UserName)
	_, err = ss.Send(
		publicKeyPath,
		remotePath,
		ssh.WithTimeout(15*time.Second),
		ssh.WithRetry(3, 3*time.Second),
	)
	if err != nil {
		return fmt.Errorf("failed to send %q to %q for %q(%q) (error %v)", publicKeyPath, remotePath, ec2Config.ClusterName, target.InstanceID, err)
	}
	copyCmd = fmt.Sprintf("sudo mkdir -p %s && sudo cp %s %s", filepath.Dir(kubeAPIServerConfig.KubeletClientCertificate), remotePath, kubeAPIServerConfig.KubeletClientCertificate)
	_, err = ss.Run(
		copyCmd,
		ssh.WithTimeout(15*time.Second),
		ssh.WithRetry(3, 3*time.Second),
	)
	if err != nil {
		return fmt.Errorf("failed to %q for %q(%q) (error %v)", copyCmd, ec2Config.ClusterName, target.InstanceID, err)
	}
	copyCmd = fmt.Sprintf("sudo mkdir -p %s && sudo cp %s %s", filepath.Dir(kubeAPIServerConfig.TLSCertFile), remotePath, kubeAPIServerConfig.TLSCertFile)
	_, err = ss.Run(
		copyCmd,
		ssh.WithTimeout(15*time.Second),
		ssh.WithRetry(3, 3*time.Second),
	)
	if err != nil {
		return fmt.Errorf("failed to %q for %q(%q) (error %v)", copyCmd, ec2Config.ClusterName, target.InstanceID, err)
	}

	remotePath = fmt.Sprintf("/home/%s/kube-apiserver.root.ca.crt", ec2Config.UserName)
	_, err = ss.Send(
		rootCAPath,
		remotePath,
		ssh.WithTimeout(15*time.Second),
		ssh.WithRetry(3, 3*time.Second),
	)
	if err != nil {
		return fmt.Errorf("failed to send %q to %q for %q(%q) (error %v)", rootCAPath, remotePath, ec2Config.ClusterName, target.InstanceID, err)
	}
	copyCmd = fmt.Sprintf("sudo mkdir -p %s && sudo cp %s %s", filepath.Dir(kubeAPIServerConfig.ClientCAFile), remotePath, kubeAPIServerConfig.ClientCAFile)
	_, err = ss.Run(
		copyCmd,
		ssh.WithTimeout(15*time.Second),
		ssh.WithRetry(3, 3*time.Second),
	)
	if err != nil {
		return fmt.Errorf("failed to %q for %q(%q) (error %v)", copyCmd, ec2Config.ClusterName, target.InstanceID, err)
	}

	return nil
}
