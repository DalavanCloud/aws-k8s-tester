package ec2

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/ec2"
	"go.uber.org/zap"
)

func (md *embedded) createKeyPair() (err error) {
	if md.cfg.KeyName == "" {
		return errors.New("cannot delete key pair without key name")
	}

	defer func() {
		if err != nil {
			os.RemoveAll(md.cfg.KeyPath)
			md.lg.Warn("removed private key since 'createKeyPair' failed", zap.Error(err))
		}
	}()

	var output *ec2.CreateKeyPairOutput
	output, err = md.ec2.CreateKeyPair(&ec2.CreateKeyPairInput{
		KeyName: aws.String(md.cfg.KeyName),
	})
	if err != nil {
		return err
	}
	if *output.KeyName != md.cfg.KeyName {
		return fmt.Errorf("unexpected key name %q, expected %q", *output.KeyName, md.cfg.KeyName)
	}
	if err = ioutil.WriteFile(md.cfg.KeyPath, []byte(*output.KeyMaterial), 0600); err != nil {
		return err
	}

	md.lg.Info(
		"created key pair",
		zap.String("key-name", md.cfg.KeyName),
		zap.String("key-path", md.cfg.KeyPath),
	)
	return md.cfg.Sync()
}

func (md *embedded) deleteKeyPair() error {
	if md.cfg.KeyName == "" {
		return errors.New("cannot delete key pair without key name")
	}

	_, err := md.ec2.DeleteKeyPair(&ec2.DeleteKeyPairInput{
		KeyName: aws.String(md.cfg.KeyName),
	})
	if err != nil {
		return err
	}

	time.Sleep(2 * time.Second)

	_, err = md.ec2.DescribeKeyPairs(&ec2.DescribeKeyPairsInput{
		KeyNames: aws.StringSlice([]string{md.cfg.KeyName}),
	})
	if err != nil {
		awsErr, ok := err.(awserr.Error)
		if ok && awsErr.Code() == "InvalidKeyPair.NotFound" {
			md.lg.Info(
				"deleted key pair",
				zap.String("key-name", md.cfg.KeyName),
			)
			return nil
		}
		return err
	}
	return fmt.Errorf("deleted key pair but %q still exists", md.cfg.KeyName)
}