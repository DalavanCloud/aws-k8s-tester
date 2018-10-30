package eks

import (
	"os"
	"reflect"
	"testing"

	"github.com/aws/aws-k8s-tester/eksconfig"
)

func TestEmbeddedCreateKeyPair(t *testing.T) {
	if os.Getenv("RUN_AWS_TESTS") != "1" {
		t.Skip()
	}

	cfg := eksconfig.NewDefault()

	ek, err := newTesterEmbedded(cfg)
	if err != nil {
		t.Fatal(err)
	}
	md, ok := ek.(*embedded)
	if !ok {
		t.Fatalf("expected '*embedded', got %v", reflect.TypeOf(ek))
	}

	if err = md.createKeyPair(); err != nil {
		t.Fatal(err)
	}
	if err = md.deleteKeyPair(); err != nil {
		t.Fatal(err)
	}
}
