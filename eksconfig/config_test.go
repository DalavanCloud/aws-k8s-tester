package eksconfig

import (
	"os"
	"reflect"
	"testing"
	"time"
)

func TestConfig(t *testing.T) {
	cfg := NewDefault()

	// supports only 58 pods per node
	cfg.WorkerNodeInstanceType = "m5.xlarge"
	cfg.WorkderNodeASGMin = 10
	cfg.WorkderNodeASGMax = 10
	cfg.ALBIngressController.TestServerReplicas = 600

	err := cfg.ValidateAndSetDefaults()
	if err == nil {
		t.Fatal("expected error")
	}
	t.Log(err)
}

func TestEnv(t *testing.T) {
	cfg := NewDefault()

	os.Setenv("AWS_K8S_TESTER_EKS_TEST_MODE", "aws-cli")
	os.Setenv("AWS_K8S_TESTER_EKS_KUBERNETES_VERSION", "1.11")
	os.Setenv("AWS_K8S_TESTER_EKS_TAG", "my-test")
	os.Setenv("AWS_K8S_TESTER_EKS_VPC_ID", "my-vpc-id")
	os.Setenv("AWS_K8S_TESTER_EKS_SUBNET_IDS", "a,b,c")
	os.Setenv("AWS_K8S_TESTER_EKS_SECURITY_GROUP_ID", "my-security-id")
	os.Setenv("AWS_K8S_TESTER_EKS_ENABLE_WORKER_NODE_HA", "false")
	os.Setenv("AWS_K8S_TESTER_EKS_ENABLE_WORKER_NODE_SSH", "true")
	os.Setenv("AWS_K8S_TESTER_EKS_CONFIG_PATH", "test-path")
	os.Setenv("AWS_K8S_TESTER_EKS_DOWN", "false")
	os.Setenv("AWS_K8S_TESTER_EKS_ALB_TARGET_TYPE", "ip")
	os.Setenv("AWS_K8S_TESTER_EKS_WORKER_NODE_ASG_MIN", "10")
	os.Setenv("AWS_K8S_TESTER_EKS_WORKER_NODE_ASG_MAX", "10")
	os.Setenv("AWS_K8S_TESTER_EKS_LOG_DEBUG", "true")
	os.Setenv("AWS_K8S_TESTER_EKS_UPLOAD_TESTER_LOGS", "false")
	os.Setenv("AWS_K8S_TESTER_EKS_UPLOAD_WORKER_NODE_LOGS", "false")
	os.Setenv("AWS_K8S_TESTER_EKS_WAIT_BEFORE_DOWN", "2h")
	os.Setenv("AWS_K8S_TESTER_EKS_ALB_UPLOAD_TESTER_LOGS", "false")
	os.Setenv("AWS_K8S_TESTER_EKS_ALB_TEST_EXPECT_QPS", "123.45")
	os.Setenv("AWS_K8S_TESTER_EKS_ALB_TEST_MODE", "nginx")
	os.Setenv("AWS_K8S_TESTER_EKS_ALB_ENABLE", "true")
	os.Setenv("AWS_K8S_TESTER_EKS_ALB_TEST_SCALABILITY", "false")
	os.Setenv("AWS_K8S_TESTER_EKS_ALB_TEST_METRICS", "false")
	os.Setenv("AWS_K8S_TESTER_EKS_ALB_INGRESS_CONTROLLER_IMAGE", "quay.io/coreos/alb-ingress-controller:1.0-beta.7")

	defer func() {
		os.Unsetenv("AWS_K8S_TESTER_EKS_TEST_MODE")
		os.Unsetenv("AWS_K8S_TESTER_EKS_KUBERNETES_VERSION")
		os.Unsetenv("AWS_K8S_TESTER_EKS_TAG")
		os.Unsetenv("AWS_K8S_TESTER_EKS_VPC_ID")
		os.Unsetenv("AWS_K8S_TESTER_EKS_SUBNET_IDs")
		os.Unsetenv("AWS_K8S_TESTER_EKS_SECURITY_GROUP_ID")
		os.Unsetenv("AWS_K8S_TESTER_EKS_ENABLE_WORKER_NODE_HA")
		os.Unsetenv("AWS_K8S_TESTER_EKS_ENABLE_WORKER_NODE_SSH")
		os.Unsetenv("AWS_K8S_TESTER_EKS_CONFIG_PATH")
		os.Unsetenv("AWS_K8S_TESTER_EKS_DOWN")
		os.Unsetenv("AWS_K8S_TESTER_EKS_ALB_TARGET_TYPE")
		os.Unsetenv("AWS_K8S_TESTER_EKS_WORKER_NODE_ASG_MIN")
		os.Unsetenv("AWS_K8S_TESTER_EKS_WORKER_NODE_ASG_MAX")
		os.Unsetenv("AWS_K8S_TESTER_EKS_LOG_DEBUG")
		os.Unsetenv("AWS_K8S_TESTER_EKS_UPLOAD_TESTER_LOGS")
		os.Unsetenv("AWS_K8S_TESTER_EKS_UPLOAD_WORKER_NODE_LOGS")
		os.Unsetenv("AWS_K8S_TESTER_EKS_WAIT_BEFORE_DOWN")
		os.Unsetenv("AWS_K8S_TESTER_EKS_ALB_UPLOAD_TESTER_LOGS")
		os.Unsetenv("AWS_K8S_TESTER_EKS_ALB_TEST_EXPECT_QPS")
		os.Unsetenv("AWS_K8S_TESTER_EKS_ALB_TEST_MODE")
		os.Unsetenv("AWS_K8S_TESTER_EKS_ALB_ENABLE")
		os.Unsetenv("AWS_K8S_TESTER_EKS_ALB_TEST_SCALABILITY")
		os.Unsetenv("AWS_K8S_TESTER_EKS_ALB_TEST_METRICS")
		os.Unsetenv("AWS_K8S_TESTER_EKS_ALB_INGRESS_CONTROLLER_IMAGE")
	}()

	if err := cfg.UpdateFromEnvs(); err != nil {
		t.Fatal(err)
	}

	if cfg.TestMode != "aws-cli" {
		t.Fatalf("cfg.TestMode expected 'aws-cli', got %q", cfg.TestMode)
	}
	if cfg.KubernetesVersion != "1.11" {
		t.Fatalf("KubernetesVersion 1.11, got %q", cfg.KubernetesVersion)
	}
	if cfg.Tag != "my-test" {
		t.Fatalf("Tag my-test, got %q", cfg.Tag)
	}
	if cfg.VPCID != "my-vpc-id" {
		t.Fatalf("VPCID my-vpc-id, got %q", cfg.VPCID)
	}
	if !reflect.DeepEqual(cfg.SubnetIDs, []string{"a", "b", "c"}) {
		t.Fatalf("SubnetIDs expected a,b,c, got %q", cfg.SubnetIDs)
	}
	if cfg.SecurityGroupID != "my-security-id" {
		t.Fatalf("SecurityGroupID my-id, got %q", cfg.SecurityGroupID)
	}
	if cfg.ConfigPath != "test-path" {
		t.Fatalf("alb configuration path expected 'test-path', got %q", cfg.ConfigPath)
	}
	if cfg.Down {
		t.Fatalf("cfg.Down expected 'false', got %v", cfg.Down)
	}
	if cfg.EnableWorkerNodeHA {
		t.Fatalf("cfg.EnableWorkerNodeHA expected 'false', got %v", cfg.EnableWorkerNodeHA)
	}
	if !cfg.EnableWorkerNodeSSH {
		t.Fatalf("cfg.EnableWorkerNodeSSH expected 'true', got %v", cfg.EnableWorkerNodeSSH)
	}
	if cfg.WorkderNodeASGMin != 10 {
		t.Fatalf("worker nodes min expected 10, got %q", cfg.WorkderNodeASGMin)
	}
	if cfg.WorkderNodeASGMax != 10 {
		t.Fatalf("worker nodes min expected 10, got %q", cfg.WorkderNodeASGMax)
	}
	if cfg.ALBIngressController.TargetType != "ip" {
		t.Fatalf("alb target type expected ip, got %q", cfg.ALBIngressController.TargetType)
	}
	if !cfg.LogDebug {
		t.Fatalf("LogDebug expected true, got %v", cfg.LogDebug)
	}
	if cfg.UploadTesterLogs {
		t.Fatalf("UploadTesterLogs expected false, got %v", cfg.UploadTesterLogs)
	}
	if cfg.ALBIngressController.UploadTesterLogs {
		t.Fatalf("UploadTesterLogs expected false, got %v", cfg.ALBIngressController.UploadTesterLogs)
	}
	if cfg.UploadWorkerNodeLogs {
		t.Fatalf("UploadWorkerNodeLogs expected false, got %v", cfg.UploadWorkerNodeLogs)
	}
	if cfg.WaitBeforeDown != 2*time.Hour {
		t.Fatalf("wait before down expected 2h, got %v", cfg.WaitBeforeDown)
	}
	if cfg.ALBIngressController.IngressControllerImage != "quay.io/coreos/alb-ingress-controller:1.0-beta.7" {
		t.Fatalf("cfg.ALBIngressController.IngressControllerImage expected 'quay.io/coreos/alb-ingress-controller:1.0-beta.7', got %q", cfg.ALBIngressController.IngressControllerImage)
	}
	if cfg.ALBIngressController.TestExpectQPS != 123.45 {
		t.Fatalf("cfg.ALBIngressController.TestExpectQPS expected 123.45, got %v", cfg.ALBIngressController.TestExpectQPS)
	}
	if cfg.ALBIngressController.TestMode != "nginx" {
		t.Fatalf("cfg.ALBIngressController.TestMode expected 'nginx', got %v", cfg.ALBIngressController.TestMode)
	}
	if !cfg.ALBIngressController.Enable {
		t.Fatalf("cfg.ALBIngressController.Enable expected 'true', got %v", cfg.ALBIngressController.Enable)
	}
	if cfg.ALBIngressController.TestScalability {
		t.Fatalf("cfg.ALBIngressController.TestScalability expected 'false', got %v", cfg.ALBIngressController.TestScalability)
	}
	if cfg.ALBIngressController.TestMetrics {
		t.Fatalf("cfg.ALBIngressController.TestMetrics expected 'false', got %v", cfg.ALBIngressController.TestMetrics)
	}
}

func Test_genClusterName(t *testing.T) {
	id1, id2 := genClusterName(genTag()), genClusterName(genTag())
	if id1 == id2 {
		t.Fatalf("expected %q != %q", id1, id2)
	}
	t.Log(id1)
	t.Log(id2)
}
