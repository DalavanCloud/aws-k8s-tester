package ec2

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/aws/awstester/internal/ec2"
	ec2config "github.com/aws/awstester/internal/ec2/config"
	"github.com/aws/awstester/internal/ec2/config/plugins"
	"github.com/aws/awstester/internal/ssh"
	"github.com/aws/awstester/pkg/fileutil"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

func newCreate() *cobra.Command {
	ac := &cobra.Command{
		Use:   "create <subcommand>",
		Short: "Create commands",
	}
	ac.AddCommand(
		newCreateConfig(),
		newCreateInstances(),
	)
	return ac
}

func newCreateConfig() *cobra.Command {
	return &cobra.Command{
		Use:   "config",
		Short: "Writes an awstester eks configuration with default values",
		Run:   configFunc,
	}
}

func configFunc(cmd *cobra.Command, args []string) {
	if path == "" {
		fmt.Fprintln(os.Stderr, "'--path' flag is not specified")
		os.Exit(1)
	}
	cfg := ec2config.NewDefault()
	cfg.ConfigPath = path
	cfg.Sync()
	fmt.Fprintf(os.Stderr, "wrote awstester eks configuration to %q\n", cfg.ConfigPath)
}

func newCreateInstances() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "instances",
		Short: "Create EC2 instances",
		Run:   createInstancesFunc,
	}
	cmd.PersistentFlags().BoolVar(&createWait, "wait", false, "true to wait until EC2 instances are ready")
	return cmd
}

var createWait bool

func createInstancesFunc(cmd *cobra.Command, args []string) {
	if !fileutil.Exist(path) {
		fmt.Fprintf(os.Stderr, "cannot find configuration %q\n", path)
		os.Exit(1)
	}

	cfg, err := ec2config.Load(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load configuration %q (%v)\n", path, err)
		os.Exit(1)
	}

	var dp ec2.Deployer
	dp, err = ec2.NewDeployer(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create EKS deployer %v\n", err)
		os.Exit(1)
	}

	if _, err = cfg.BackupConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to back up original config file %v\n", err)
		os.Exit(1)
	}
	if err = dp.Create(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to create instances %v\n", err)
		os.Exit(1)
	}

	fmt.Println(dp.GenerateSSHCommands())

	if createWait {
		wait(cfg)
	}

	fmt.Println(dp.GenerateSSHCommands())
}

func wait(cfg *ec2config.Config) {
	lg, err := zap.NewProduction()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create logger (%v)\n", err)
		os.Exit(1)
	}

	notifier := make(chan os.Signal, 1)
	signal.Notify(notifier, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(notifier)

	mm := cfg.InstanceIDToInstance

exit:
	for {
	doneAll:
		for id, iv := range mm {
			lg.Info("waiting for EC2", zap.String("instance-id", id))
			sh, serr := ssh.New(ssh.Config{
				Logger:   lg,
				KeyPath:  cfg.KeyPath,
				Addr:     iv.PublicIP + ":22",
				UserName: cfg.UserName,
			})
			if serr != nil {
				fmt.Fprintf(os.Stderr, "failed to create SSH (%v)\n", err)
				os.Exit(1)
			}

			if err = sh.Connect(); err != nil {
				fmt.Fprintf(os.Stderr, "failed to connect SSH (%v)\n", err)
				os.Exit(1)
			}

			var out []byte
			for {
				select {
				case sig := <-notifier:
					fmt.Fprintf(os.Stderr, "interruptted (%q)\n", sig.String())
					break exit

				case <-time.After(5 * time.Second):
					out, err = sh.Run(
						"tail -10 /var/log/cloud-init-output.log",
						ssh.WithRetry(100, 5*time.Second),
						ssh.WithTimeout(30*time.Second),
					)
					if err != nil {
						lg.Warn("failed to fetch cloud-init-output.log", zap.Error(err))
						sh.Close()
						if serr := sh.Connect(); serr != nil {
							fmt.Fprintf(os.Stderr, "failed to connect SSH (%v)\n", serr)
							break doneAll
						}
						continue
					}

					fmt.Printf("\n\n%s\n\n", string(out))

					if isReady(string(out)) {
						sh.Close()
						lg.Info("cloud-init-output.log READY!", zap.String("instance-id", id))
						delete(mm, id)
						continue doneAll
					}

					lg.Info("cloud-init-output NOT READY", zap.String("instance-id", id))
				}
			}
		}
		if len(mm) == 0 {
			lg.Info("all ready")
			break
		}
	}
}

/*
to match:

AWSTESTER_EC2_PLUGIN_READY
Cloud-init v. 18.2 running 'modules:final' at Mon, 29 Oct 2018 22:40:13 +0000. Up 21.89 seconds.
Cloud-init v. 18.2 finished at Mon, 29 Oct 2018 22:43:59 +0000. Datasource DataSourceEc2Local.  Up 246.57 seconds
*/
func isReady(txt string) bool {
	return strings.Contains(txt, plugins.READY) ||
		(strings.Contains(txt, `Cloud-init v.`) &&
			strings.Contains(txt, `finished at`))
}
