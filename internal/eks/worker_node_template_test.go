package eks

import (
	"fmt"
	"strings"
	"testing"
)

func TestWorkerNodeTemplate(t *testing.T) {
	v := workerNodeStack{
		Description:         "test",
		TagKey:              "aws-k8s-tester",
		TagValue:            "aws-k8s-tester",
		Hostname:            "hostname",
		EnableWorkerNodeSSH: true,
	}
	s, err := _createWorkerNodeTemplate(v)
	if err != nil {
		t.Fatal(err)
	}
	if v.EnableWorkerNodeSSH && !strings.Contains(s, "ClusterControlPlaneSecurityGroupIngress22") {
		t.Fatal("expected 'ClusterControlPlaneSecurityGroupIngress22'")
	}
	fmt.Println(s)
}
