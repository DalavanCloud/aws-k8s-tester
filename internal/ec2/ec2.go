package ec2

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	ec2config "github.com/aws/aws-k8s-tester/internal/ec2/config"
	"github.com/aws/aws-k8s-tester/internal/ec2/config/plugins"
	"github.com/aws/aws-k8s-tester/internal/ssh"
	"github.com/aws/aws-k8s-tester/pkg/awsapi"
	"github.com/aws/aws-k8s-tester/pkg/zaputil"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/cloudformation"
	"github.com/aws/aws-sdk-go/service/cloudformation/cloudformationiface"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/ec2/ec2iface"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3iface"
	"github.com/aws/aws-sdk-go/service/sts"
	"github.com/aws/aws-sdk-go/service/sts/stsiface"
	"github.com/dustin/go-humanize"
	"go.uber.org/zap"
)

// Deployer defines EC2 deployer.
type Deployer interface {
	Create() error
	Stop()
	Delete() error

	Logger() *zap.Logger
	GenerateSSHCommands() string
}

type embedded struct {
	stopc chan struct{}

	mu  sync.RWMutex
	lg  *zap.Logger
	cfg *ec2config.Config

	ss  *session.Session
	sts stsiface.STSAPI
	cf  cloudformationiface.CloudFormationAPI
	ec2 ec2iface.EC2API

	s3        s3iface.S3API
	s3Buckets map[string]struct{}
}

// TODO: use cloudformation

// NewDeployer creates a new EKS deployer.
func NewDeployer(cfg *ec2config.Config) (Deployer, error) {
	if err := cfg.ValidateAndSetDefaults(); err != nil {
		return nil, err
	}

	now := time.Now().UTC()

	lg, err := zaputil.New(cfg.LogDebug, cfg.LogOutputs)
	if err != nil {
		return nil, err
	}

	md := &embedded{
		stopc:     make(chan struct{}),
		lg:        lg,
		cfg:       cfg,
		s3Buckets: make(map[string]struct{}),
	}

	awsCfg := &awsapi.Config{
		Logger:         md.lg,
		DebugAPICalls:  cfg.LogDebug,
		Region:         cfg.AWSRegion,
		CustomEndpoint: "",
	}
	md.ss, err = awsapi.New(awsCfg)
	if err != nil {
		return nil, err
	}

	md.sts = sts.New(md.ss)
	md.cf = cloudformation.New(md.ss)
	md.ec2 = ec2.New(md.ss)
	md.s3 = s3.New(md.ss)

	output, oerr := md.sts.GetCallerIdentity(&sts.GetCallerIdentityInput{})
	if oerr != nil {
		return nil, oerr
	}
	md.cfg.AWSAccountID = *output.Account

	lg.Info(
		"created EC2 deployer",
		zap.String("id", cfg.ID),
		zap.String("aws-k8s-tester-ec2-config-path", cfg.ConfigPath),
		zap.String("request-started", humanize.RelTime(now, time.Now().UTC(), "ago", "from now")),
	)
	return md, md.cfg.Sync()
}

func (md *embedded) Create() (err error) {
	md.mu.Lock()
	defer md.mu.Unlock()

	now := time.Now().UTC()
	md.lg.Info("creating", zap.String("id", md.cfg.ID))

	defer func() {
		if err != nil {
			md.lg.Warn("reverting EC2 creation", zap.Error(err))
			if derr := md.deleteInstances(); derr != nil {
				md.lg.Warn("failed to revert instance creation", zap.Error(derr))
			}
			if derr := md.deleteSecurityGroup(); derr != nil {
				md.lg.Warn("failed to revert security group creation", zap.Error(derr))
			}
			if md.cfg.VPCCreated {
				if derr := md.deleteSubnet(); derr != nil {
					md.lg.Warn("failed to revert subnet creation", zap.Error(derr))
				}
				if derr := md.deleteVPC(); derr != nil {
					md.lg.Warn("failed to revert VPC creation", zap.Error(derr))
				}
			}
			if derr := md.deleteKeyPair(); derr != nil {
				md.lg.Warn("failed to revert key pair creation", zap.Error(derr))
			}
		}
	}()
	defer md.cfg.Sync()

	if err = catchStopc(md.lg, md.stopc, md.createKeyPair); err != nil {
		return err
	}
	if md.cfg.VPCID != "" { // use existing VPC
		if len(md.cfg.SubnetIDs) == 0 {
			if err = catchStopc(md.lg, md.stopc, md.getSubnets); err != nil {
				return err
			}
		} else {
			// otherwise, use specified subnet ID
			var output *ec2.DescribeSubnetsOutput
			output, err = md.ec2.DescribeSubnets(&ec2.DescribeSubnetsInput{
				SubnetIds: aws.StringSlice(md.cfg.SubnetIDs),
			})
			if err != nil {
				return err
			}
			if md.cfg.SubnetIDToAvailibilityZone == nil {
				md.cfg.SubnetIDToAvailibilityZone = make(map[string]string)
			}
			for _, sv := range output.Subnets {
				md.cfg.SubnetIDToAvailibilityZone[*sv.SubnetId] = *sv.AvailabilityZone
			}
		}

		md.lg.Info("modifying subnets to allow SSH access")
		if err = md.modifySubnets(); err != nil {
			return err
		}
		md.lg.Info(
			"found subnets",
			zap.String("vpc-id", md.cfg.VPCID),
			zap.Strings("subnet-ids", md.cfg.SubnetIDs),
			zap.String("availability-zones", fmt.Sprintf("%v", md.cfg.SubnetIDToAvailibilityZone)),
		)
	} else {
		if err = catchStopc(md.lg, md.stopc, md.createVPC); err != nil {
			return err
		}
		md.cfg.VPCCreated = true
		md.cfg.Sync()
		if err = catchStopc(md.lg, md.stopc, md.createSubnets); err != nil {
			return err
		}
	}
	if err = catchStopc(md.lg, md.stopc, md.createSecurityGroup); err != nil {
		return err
	}
	if err = catchStopc(md.lg, md.stopc, md.createInstances); err != nil {
		return err
	}

	md.lg.Info("created",
		zap.String("id", md.cfg.ID),
		zap.String("request-started", humanize.RelTime(now, time.Now().UTC(), "ago", "from now")),
	)

	if err = md.cfg.Sync(); err != nil {
		return err
	}
	if md.cfg.UploadTesterLogs {
		if err = md.uploadTesterLogs(); err != nil {
			md.lg.Warn("failed to upload", zap.Error(err))
		}
	}

	return nil
}

func (md *embedded) Stop() { close(md.stopc) }

func (md *embedded) Delete() (err error) {
	md.mu.Lock()
	defer md.mu.Unlock()

	now := time.Now().UTC()
	md.lg.Info("deleting", zap.String("id", md.cfg.ID))

	var errs []string
	if err = md.deleteInstances(); err != nil {
		md.lg.Warn("failed to delete instances", zap.Error(err))
		errs = append(errs, err.Error())
	}
	if err = md.deleteSecurityGroup(); err != nil {
		md.lg.Warn("failed to delete security group", zap.Error(err))
		errs = append(errs, err.Error())
	}
	if md.cfg.VPCCreated {
		if err = md.deleteSubnet(); err != nil {
			md.lg.Warn("failed to delete subnet", zap.Error(err))
			errs = append(errs, err.Error())
		}
		if err = md.deleteVPC(); err != nil {
			md.lg.Warn("failed to delete VPC", zap.Error(err))
			errs = append(errs, err.Error())
		}
	}
	if err = md.deleteKeyPair(); err != nil {
		md.lg.Warn("failed to delete key pair", zap.Error(err))
		errs = append(errs, err.Error())
	}

	if len(errs) > 0 {
		return errors.New(strings.Join(errs, ", "))
	}

	md.lg.Info("deleted",
		zap.String("id", md.cfg.ID),
		zap.String("request-started", humanize.RelTime(now, time.Now().UTC(), "ago", "from now")),
	)

	if err = md.cfg.Sync(); err != nil {
		return err
	}
	if md.cfg.UploadTesterLogs {
		if err = md.uploadTesterLogs(); err != nil {
			md.lg.Warn("failed to upload", zap.Error(err))
		}
	}

	return nil
}

func (md *embedded) uploadTesterLogs() (err error) {
	if err = md.toS3(
		md.cfg.ConfigPath,
		md.cfg.ConfigPathBucket,
	); err != nil {
		return err
	}
	if err = md.toS3(
		md.cfg.LogOutputToUploadPath,
		md.cfg.LogOutputToUploadPathBucket,
	); err != nil {
		return err
	}
	return md.toS3(
		md.cfg.KeyPath,
		md.cfg.KeyPathBucket,
	)
}

func (md *embedded) createInstances() (err error) {
	now := time.Now().UTC()

	// evenly distribute per subnet
	left := md.cfg.Count

	tokens := []string{}
	tknToCnt := make(map[string]int)
	h, _ := os.Hostname()

	if md.cfg.Count > len(md.cfg.SubnetIDs) {
		// TODO: configure this per EC2 quota?
		runInstancesBatch := 7
		subnetAllocBatch := md.cfg.Count / len(md.cfg.SubnetIDs)

		subnetIdx := 0
		for left > 0 {
			n := subnetAllocBatch
			if subnetAllocBatch > left {
				n = left
			}
			md.lg.Info(
				"creating an EC2 instance",
				zap.Int("count", n),
				zap.Int("left", left),
				zap.Int("target-total", md.cfg.Count),
			)

			subnetID := md.cfg.SubnetIDs[0]
			if len(md.cfg.SubnetIDs) > 1 {
				subnetID = md.cfg.SubnetIDs[subnetIdx%len(md.cfg.SubnetIDs)]
			}

			if n < runInstancesBatch {
				tkn := md.cfg.ID + fmt.Sprintf("%X", time.Now().Nanosecond())
				tokens = append(tokens, tkn)

				_, err = md.ec2.RunInstances(&ec2.RunInstancesInput{
					ClientToken:                       aws.String(tkn),
					ImageId:                           aws.String(md.cfg.ImageID),
					MinCount:                          aws.Int64(int64(n)),
					MaxCount:                          aws.Int64(int64(n)),
					InstanceType:                      aws.String(md.cfg.InstanceType),
					KeyName:                           aws.String(md.cfg.KeyName),
					SubnetId:                          aws.String(subnetID),
					SecurityGroupIds:                  aws.StringSlice(md.cfg.SecurityGroupIDs),
					InstanceInitiatedShutdownBehavior: aws.String("terminate"),
					UserData:                          aws.String(base64.StdEncoding.EncodeToString([]byte(md.cfg.InitScript))),
					TagSpecifications: []*ec2.TagSpecification{
						{
							ResourceType: aws.String("instance"),
							Tags: []*ec2.Tag{
								{
									Key:   aws.String(md.cfg.Tag),
									Value: aws.String(md.cfg.Tag),
								},
								{
									Key:   aws.String("HOSTNAME"),
									Value: aws.String(h),
								},
							},
						},
					},
				})
				if err != nil {
					return err
				}
				tknToCnt[tkn] = n
			} else {
				nLeft := n
				for nLeft > 0 {
					tkn := md.cfg.ID + fmt.Sprintf("%X", time.Now().UTC().Nanosecond())
					tokens = append(tokens, tkn)

					x := runInstancesBatch
					if nLeft < runInstancesBatch {
						x = nLeft
					}
					nLeft -= x

					_, err = md.ec2.RunInstances(&ec2.RunInstancesInput{
						ClientToken:                       aws.String(tkn),
						ImageId:                           aws.String(md.cfg.ImageID),
						MinCount:                          aws.Int64(int64(x)),
						MaxCount:                          aws.Int64(int64(x)),
						InstanceType:                      aws.String(md.cfg.InstanceType),
						KeyName:                           aws.String(md.cfg.KeyName),
						SubnetId:                          aws.String(subnetID),
						SecurityGroupIds:                  aws.StringSlice(md.cfg.SecurityGroupIDs),
						InstanceInitiatedShutdownBehavior: aws.String("terminate"),
						UserData:                          aws.String(base64.StdEncoding.EncodeToString([]byte(md.cfg.InitScript))),
						TagSpecifications: []*ec2.TagSpecification{
							{
								ResourceType: aws.String("instance"),
								Tags: []*ec2.Tag{
									{
										Key:   aws.String(md.cfg.Tag),
										Value: aws.String(md.cfg.Tag),
									},
									{
										Key:   aws.String("HOSTNAME"),
										Value: aws.String(h),
									},
								},
							},
						},
					})
					if err != nil {
						return err
					}

					tknToCnt[tkn] = x
					md.lg.Info("launched a batch of instances", zap.Int("instance-count", x))

					time.Sleep(10 * time.Second)
				}
			}

			subnetIdx++
			left -= subnetAllocBatch

			md.lg.Info(
				"created EC2 instance group",
				zap.String("subnet-id", subnetID),
				zap.String("availability-zone", md.cfg.SubnetIDToAvailibilityZone[subnetID]),
				zap.Int("instance-count", n),
			)
		}
	} else {
		// create <1 instance per subnet
		for i := 0; i < md.cfg.Count; i++ {
			tkn := md.cfg.ID + fmt.Sprintf("%X", time.Now().Nanosecond())
			tokens = append(tokens, tkn)
			tknToCnt[tkn] = 1

			subnetID := md.cfg.SubnetIDs[0]
			if len(md.cfg.SubnetIDs) > 1 {
				subnetID = md.cfg.SubnetIDs[i%len(md.cfg.SubnetIDs)]
			}

			_, err = md.ec2.RunInstances(&ec2.RunInstancesInput{
				ClientToken:                       aws.String(tkn),
				ImageId:                           aws.String(md.cfg.ImageID),
				MinCount:                          aws.Int64(1),
				MaxCount:                          aws.Int64(1),
				InstanceType:                      aws.String(md.cfg.InstanceType),
				KeyName:                           aws.String(md.cfg.KeyName),
				SubnetId:                          aws.String(subnetID),
				SecurityGroupIds:                  aws.StringSlice(md.cfg.SecurityGroupIDs),
				InstanceInitiatedShutdownBehavior: aws.String("terminate"),
				UserData:                          aws.String(base64.StdEncoding.EncodeToString([]byte(md.cfg.InitScript))),
				TagSpecifications: []*ec2.TagSpecification{
					{
						ResourceType: aws.String("instance"),
						Tags: []*ec2.Tag{
							{
								Key:   aws.String(md.cfg.Tag),
								Value: aws.String(md.cfg.Tag),
							},
							{
								Key:   aws.String("HOSTNAME"),
								Value: aws.String(h),
							},
						},
					},
				},
			})
			if err != nil {
				return err
			}

			md.lg.Info(
				"created EC2 instance group",
				zap.String("subnet-id", subnetID),
				zap.String("availability-zone", md.cfg.SubnetIDToAvailibilityZone[subnetID]),
			)
		}
	}

	md.cfg.InstanceIDToInstance = make(map[string]ec2config.Instance)

	tknToCntRunning := make(map[string]int)
	retryStart := time.Now().UTC()
	for len(md.cfg.Instances) != md.cfg.Count &&
		time.Now().UTC().Sub(retryStart) < time.Duration(md.cfg.Count)*2*time.Minute {
		for _, tkn := range tokens {
			if v, ok := tknToCntRunning[tkn]; ok {
				if v == tknToCnt[tkn] {
					continue
				}
			}

			var output *ec2.DescribeInstancesOutput
			output, err = md.ec2.DescribeInstances(&ec2.DescribeInstancesInput{
				Filters: []*ec2.Filter{
					{
						Name:   aws.String("client-token"),
						Values: aws.StringSlice([]string{tkn}),
					},
				},
			})
			if err != nil {
				md.lg.Warn("failed to describe instances", zap.Error(err))
				continue
			}

			for _, rv := range output.Reservations {
				for _, inst := range rv.Instances {
					id := *inst.InstanceId
					if *inst.State.Name == "running" {
						_, ok := md.cfg.InstanceIDToInstance[id]
						if !ok {
							iv := ec2config.ConvertEC2Instance(inst)
							md.cfg.Instances = append(md.cfg.Instances, iv)
							md.cfg.InstanceIDToInstance[id] = iv
							md.lg.Info("instance is ready",
								zap.String("instance-id", iv.InstanceID),
								zap.String("instance-public-ip", iv.PublicIP),
								zap.String("instance-public-dns", iv.PublicDNSName),
							)
							tknToCntRunning[tkn]++

							if v, ok := tknToCntRunning[tkn]; ok {
								if v == tknToCnt[tkn] {
									md.lg.Info("instance group is ready", zap.String("client-token", tkn), zap.Int("count", v))
								}
							}
						}
					}
				}
			}

			time.Sleep(5 * time.Second)
		}
	}

	// sort that first launched EC2 instance is at front
	sort.Sort(ec2config.Instances(md.cfg.Instances))

	md.lg.Info(
		"created EC2 instances",
		zap.Int("count", md.cfg.Count),
		zap.String("request-started", humanize.RelTime(now, time.Now().UTC(), "ago", "from now")),
	)

	if md.cfg.Wait {
		md.cfg.Sync()
		md.lg.Info(
			"waiting for EC2 instances",
			zap.Int("count", md.cfg.Count),
			zap.String("request-started", humanize.RelTime(now, time.Now().UTC(), "ago", "from now")),
		)
	}
	return md.cfg.Sync()
}

func (md *embedded) wait() {
	mm := make(map[string]ec2config.Instance, len(md.cfg.InstanceIDToInstance))
	for k, v := range md.cfg.InstanceIDToInstance {
		mm[k] = v
	}

	for {
	done:
		for id, iv := range mm {
			md.lg.Info("waiting for EC2", zap.String("instance-id", id))
			sh, serr := ssh.New(ssh.Config{
				Logger:   md.lg,
				KeyPath:  md.cfg.KeyPath,
				Addr:     iv.PublicIP + ":22",
				UserName: md.cfg.UserName,
			})
			if serr != nil {
				fmt.Fprintf(os.Stderr, "failed to create SSH (%v)\n", serr)
				os.Exit(1)
			}

			if err := sh.Connect(); err != nil {
				fmt.Fprintf(os.Stderr, "failed to connect SSH (%v)\n", err)
				os.Exit(1)
			}

			var out []byte
			var err error
			for {
				select {
				case <-time.After(5 * time.Second):
					out, err = sh.Run(
						"tail -10 /var/log/cloud-init-output.log",
						ssh.WithRetry(100, 5*time.Second),
						ssh.WithTimeout(30*time.Second),
					)
					if err != nil {
						md.lg.Warn("failed to fetch cloud-init-output.log", zap.Error(err))
						sh.Close()
						if serr := sh.Connect(); serr != nil {
							fmt.Fprintf(os.Stderr, "failed to connect SSH (%v)\n", serr)
							continue done
						}
						continue
					}

					fmt.Printf("\n\n%s\n\n", string(out))

					if isReady(string(out)) {
						sh.Close()
						md.lg.Info("cloud-init-output.log READY!", zap.String("instance-id", id))
						delete(mm, id)
						continue done
					}

					md.lg.Info("cloud-init-output NOT READY", zap.String("instance-id", id))
				}
			}
		}
		if len(mm) == 0 {
			md.lg.Info("all EC2 instances are ready")
			break
		}
	}
}

/*
to match:

AWS_K8S_TESTER_EC2_PLUGIN_READY
Cloud-init v. 18.2 running 'modules:final' at Mon, 29 Oct 2018 22:40:13 +0000. Up 21.89 seconds.
Cloud-init v. 18.2 finished at Mon, 29 Oct 2018 22:43:59 +0000. Datasource DataSourceEc2Local.  Up 246.57 seconds
*/
func isReady(txt string) bool {
	return strings.Contains(txt, plugins.READY) ||
		(strings.Contains(txt, `Cloud-init v.`) &&
			strings.Contains(txt, `finished at`))
}

func (md *embedded) deleteInstances() (err error) {
	now := time.Now().UTC()

	ids := make([]string, 0, len(md.cfg.Instances))
	for _, iv := range md.cfg.Instances {
		ids = append(ids, iv.InstanceID)
	}
	_, err = md.ec2.TerminateInstances(&ec2.TerminateInstancesInput{
		InstanceIds: aws.StringSlice(ids),
	})
	md.lg.Info("terminating", zap.Strings("instance-ids", ids), zap.Error(err))

	sleepDur := 5 * time.Second * time.Duration(md.cfg.Count)
	if sleepDur > 3*time.Minute {
		sleepDur = 3 * time.Minute
	}
	time.Sleep(sleepDur)

	retryStart := time.Now().UTC()
	terminated := make(map[string]struct{})
	for len(terminated) != md.cfg.Count &&
		time.Now().UTC().Sub(retryStart) < time.Duration(md.cfg.Count)*2*time.Minute {
		var output *ec2.DescribeInstancesOutput
		output, err = md.ec2.DescribeInstances(&ec2.DescribeInstancesInput{
			InstanceIds: aws.StringSlice(ids),
		})
		if err != nil {
			return err
		}

		for _, rv := range output.Reservations {
			for _, inst := range rv.Instances {
				id := *inst.InstanceId
				if _, ok := terminated[id]; ok {
					continue
				}
				if *inst.State.Name == "terminated" {
					terminated[id] = struct{}{}
					md.lg.Info("terminated", zap.String("instance-id", id))
				}
			}
		}

		time.Sleep(5 * time.Second)
	}

	md.lg.Info("terminated",
		zap.Strings("instance-ids", ids),
		zap.String("request-started", humanize.RelTime(now, time.Now().UTC(), "ago", "from now")),
	)
	return nil
}

func (md *embedded) Logger() *zap.Logger {
	return md.lg
}

func (md *embedded) GenerateSSHCommands() (s string) {
	s = fmt.Sprintf("\n\n# change SSH key permission\nchmod 400 %s\n\n", md.cfg.KeyPath)
	for _, v := range md.cfg.Instances {
		s += fmt.Sprintf(`ssh -o "StrictHostKeyChecking no" -i %s %s@%s
`, md.cfg.KeyPath, md.cfg.UserName, v.PublicDNSName)
	}
	return s
}

func catchStopc(lg *zap.Logger, stopc chan struct{}, run func() error) (err error) {
	errc := make(chan error)
	go func() {
		errc <- run()
	}()
	select {
	case <-stopc:
		lg.Info("interrupting")
		gerr := <-errc
		lg.Info("interrupted", zap.Error(gerr))
		err = fmt.Errorf("interrupted (run function returned %v)", gerr)
	case err = <-errc:
		if err != nil {
			return err
		}
	}
	return err
}
