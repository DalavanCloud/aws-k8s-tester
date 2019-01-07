package kubernetesconfig

import (
	"bytes"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"text/template"
)

// KubeScheduler represents "kube-scheduler" configuration.
type KubeScheduler struct {
	Path           string `json:"path"`
	DownloadURL    string `json:"download-url"`
	VersionCommand string `json:"version-command"`

	// TODO: support running as a static pod
	// Image is the container image name and tag for kube-scheduler to run as a static pod.
	// Image string `json:"image"`

	// UserName is the user name used for running init scripts or SSH access.
	UserName string `json:"user-name,omitempty"`

	Kubeconfig  string `json:"kubeconfig" kube-scheduler:"kubeconfig"`
	LeaderElect bool   `json:"leader-elect" kube-scheduler:"leader-elect"`
}

var defaultKubeScheduler = KubeScheduler{
	Path:           "/usr/bin/kube-scheduler",
	DownloadURL:    fmt.Sprintf("https://storage.googleapis.com/kubernetes-release/release/v%s/bin/linux/amd64/kube-scheduler", defaultKubernetesVersion),
	VersionCommand: "/usr/bin/kube-scheduler --version",

	Kubeconfig:  "/var/lib/kube-scheduler/kubeconfig",
	LeaderElect: true,
}

func newDefaultKubeScheduler() *KubeScheduler {
	copied := defaultKubeScheduler
	return &copied
}

func (kb *KubeScheduler) updateFromEnvs(pfx string) error {
	cc := *kb
	tp, vv := reflect.TypeOf(&cc).Elem(), reflect.ValueOf(&cc).Elem()
	for i := 0; i < tp.NumField(); i++ {
		jv := tp.Field(i).Tag.Get("json")
		if jv == "" {
			continue
		}
		jv = strings.Replace(jv, ",omitempty", "", -1)
		jv = strings.Replace(jv, "-", "_", -1)
		jv = strings.ToUpper(strings.Replace(jv, "-", "_", -1))
		env := pfx + jv
		if os.Getenv(env) == "" {
			continue
		}
		sv := os.Getenv(env)

		switch vv.Field(i).Type().Kind() {
		case reflect.String:
			vv.Field(i).SetString(sv)

		case reflect.Bool:
			bb, err := strconv.ParseBool(sv)
			if err != nil {
				return fmt.Errorf("failed to parse %q (%q, %v)", sv, env, err)
			}
			vv.Field(i).SetBool(bb)

		case reflect.Int, reflect.Int32, reflect.Int64:
			// if tp.Field(i).Name { continue }
			iv, err := strconv.ParseInt(sv, 10, 64)
			if err != nil {
				return fmt.Errorf("failed to parse %q (%q, %v)", sv, env, err)
			}
			vv.Field(i).SetInt(iv)

		case reflect.Uint, reflect.Uint32, reflect.Uint64:
			iv, err := strconv.ParseUint(sv, 10, 64)
			if err != nil {
				return fmt.Errorf("failed to parse %q (%q, %v)", sv, env, err)
			}
			vv.Field(i).SetUint(iv)

		case reflect.Float32, reflect.Float64:
			fv, err := strconv.ParseFloat(sv, 64)
			if err != nil {
				return fmt.Errorf("failed to parse %q (%q, %v)", sv, env, err)
			}
			vv.Field(i).SetFloat(fv)

		case reflect.Slice:
			ss := strings.Split(sv, ",")
			slice := reflect.MakeSlice(reflect.TypeOf([]string{}), len(ss), len(ss))
			for i := range ss {
				slice.Index(i).SetString(ss[i])
			}
			vv.Field(i).Set(slice)

		default:
			return fmt.Errorf("%q (%v) is not supported as an env", env, vv.Field(i).Type())
		}
	}
	*kb = cc
	return nil
}

// Service returns a script to configure kube-scheduler systemd service file.
func (kb *KubeScheduler) Service() (s string, err error) {
	tpl := template.Must(template.New("kubeSchedulerTemplate").Parse(kubeSchedulerTemplate))
	buf := bytes.NewBuffer(nil)
	kv := kubeSchedulerTemplateInfo{KubeSchedulerPath: kb.Path}
	if err := tpl.Execute(buf, kv); err != nil {
		return "", err
	}
	return buf.String(), nil
}

type kubeSchedulerTemplateInfo struct {
	KubeSchedulerPath string
}

const kubeSchedulerTemplate = `#!/usr/bin/env bash

sudo systemctl stop kube-scheduler.service || true

sudo mkdir -p /var/lib/kube-scheduler/

rm -f /tmp/kube-scheduler.service
cat <<EOF > /tmp/kube-scheduler.service
[Unit]
Description=kube-scheduler: The Kubernetes API Server
Documentation=http://kubernetes.io/docs/
After=docker.service

[Service]
EnvironmentFile=/etc/sysconfig/kube-scheduler
ExecStart={{ .KubeSchedulerPath }} "\$KUBE_SCHEDULER_FLAGS"
Restart=always
RestartSec=2s
StartLimitInterval=0
KillMode=process
User=root

[Install]
WantedBy=multi-user.target
EOF
cat /tmp/kube-scheduler.service

sudo mkdir -p /etc/systemd/system/kube-scheduler.service.d
sudo cp /tmp/kube-scheduler.service /etc/systemd/system/kube-scheduler.service

sudo systemctl daemon-reload
sudo systemctl cat kube-scheduler.service
`

// Sysconfig returns "/etc/sysconfig/kube-scheduler" file.
func (kb *KubeScheduler) Sysconfig() (s string, err error) {
	var fs []string
	fs, err = kb.Flags()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(`KUBE_SCHEDULER_FLAGS="%s"
HOME="/home/%s"
`, strings.Join(fs, " "),
		kb.UserName,
	), nil
}

// Flags returns the list of "kube-scheduler" flags.
// Make sure to validate the configuration with "ValidateAndSetDefaults".
func (kb *KubeScheduler) Flags() (flags []string, err error) {
	tp, vv := reflect.TypeOf(kb).Elem(), reflect.ValueOf(kb).Elem()
	for i := 0; i < tp.NumField(); i++ {
		k := tp.Field(i).Tag.Get("kube-scheduler")
		if k == "" {
			continue
		}
		allowZeroValue := tp.Field(i).Tag.Get("allow-zero-value") == "true"

		switch vv.Field(i).Type().Kind() {
		case reflect.String:
			if vv.Field(i).String() != "" {
				flags = append(flags, fmt.Sprintf("--%s=%s", k, vv.Field(i).String()))
			} else if allowZeroValue {
				flags = append(flags, fmt.Sprintf(`--%s=""`, k))
			}

		case reflect.Int, reflect.Int32, reflect.Int64:
			if vv.Field(i).String() != "" {
				flags = append(flags, fmt.Sprintf("--%s=%d", k, vv.Field(i).Int()))
			} else if allowZeroValue {
				flags = append(flags, fmt.Sprintf(`--%s=0`, k))
			}

		case reflect.Bool:
			flags = append(flags, fmt.Sprintf("--%s=%v", k, vv.Field(i).Bool()))

		default:
			return nil, fmt.Errorf("unknown %q", k)
		}
	}
	return flags, nil
}
