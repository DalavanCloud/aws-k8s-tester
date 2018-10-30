package ec2

import (
	"fmt"
	"os"

	"github.com/aws/aws-k8s-tester/internal/ec2"
	ec2config "github.com/aws/aws-k8s-tester/internal/ec2/config"
	"github.com/aws/aws-k8s-tester/pkg/fileutil"

	"github.com/spf13/cobra"
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
		Short: "Writes an aws-k8s-tester eks configuration with default values",
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
	fmt.Fprintf(os.Stderr, "wrote aws-k8s-tester eks configuration to %q\n", cfg.ConfigPath)
}

func newCreateInstances() *cobra.Command {
	return &cobra.Command{
		Use:   "instances",
		Short: "Create EC2 instances",
		Run:   createInstancesFunc,
	}
}

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
}
