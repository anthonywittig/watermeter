package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/lambda/types"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

type lambdaDeployer struct {
	cfg        aws.Config
	name       string
	projectDir string
	sqs        *sqs.Client
	svc        *lambda.Client
}

func main() {
	ctx := context.Background()
	profile := os.Args[1]
	name := os.Args[2]
	token := os.Args[3]

	if profile == "" {
		log.Fatal("profile must be set")
	}

	projectDir, err := filepath.Abs("./../../")
	if err != nil {
		log.Fatalf("unable to create project directory path, %v", err)
	}

	fmt.Printf("Deploying lambda: %s\n", name)

	cfg, err := config.LoadDefaultConfig(
		ctx,
		config.WithSharedConfigProfile(profile),
		config.WithAssumeRoleCredentialOptions(func(o *stscreds.AssumeRoleOptions) {
			o.TokenProvider = func() (string, error) {
				return token, nil
			}
		}),
	)
	if err != nil {
		log.Fatalf("unable to load SDK config, %v", err)
	}

	svc := lambda.NewFromConfig(cfg)
	sqs := sqs.NewFromConfig(cfg)

	l := lambdaDeployer{
		cfg:        cfg,
		name:       name,
		projectDir: projectDir,
		sqs:        sqs,
		svc:        svc,
	}

	if err := l.createIfNotExists(ctx); err != nil {
		log.Fatalf("unable to create if not exists, %v", err)
	}

	if err := l.updateCode(ctx); err != nil {
		log.Fatalf("unable to update code, %v", err)
	}

	if err := l.configureSQS(ctx); err != nil {
		log.Fatalf("unable to configure sqs, %v", err)
	}
}

func (l *lambdaDeployer) getZip() ([]byte, error) {
	buildDir := fmt.Sprintf("%s/build/%s", l.projectDir, l.name)
	zipContents, err := ioutil.ReadFile(fmt.Sprintf("%s/function.zip", buildDir))
	if err != nil {
		return []byte{}, fmt.Errorf("error reading zip file, %v", err)
	}

	return zipContents, nil
}

func (l *lambdaDeployer) createIfNotExists(ctx context.Context) error {
	listResp, err := l.svc.ListFunctions(ctx, &lambda.ListFunctionsInput{})
	if err != nil {
		return fmt.Errorf("unable to list functions, %v", err)
	}

	for _, fn := range listResp.Functions {
		if *fn.FunctionName == l.name {
			return nil
		}
	}

	zipContents, err := l.getZip()
	if err != nil {
		return fmt.Errorf("unable to build and get zip, %v", err)
	}

	role, err := l.createRoleIfNotExists(ctx)
	if err != nil {
		return fmt.Errorf("unable to create role if not exists, %v", err)
	}

	if _, err := l.svc.CreateFunction(
		ctx,
		&lambda.CreateFunctionInput{
			Code: &types.FunctionCode{
				ZipFile: zipContents,
			},
			FunctionName: &l.name,
			Role:         role,
			Handler:      aws.String("main"),
			PackageType:  "Zip",
			Publish:      true,
			Runtime:      "go1.x",
			Timeout:      aws.Int32(30), // Seems like an ok default, but some will need more.
		},
	); err != nil {
		return fmt.Errorf("unable to create function, %v", err)
	}

	return nil
}

func (l *lambdaDeployer) createRoleIfNotExists(ctx context.Context) (*string, error) {
	iamSvc := iam.NewFromConfig(l.cfg)

	roleName := fmt.Sprintf("%s-lambda-role", l.name)

	list, err := iamSvc.ListRoles(ctx, &iam.ListRolesInput{})
	if err != nil {
		return nil, fmt.Errorf("unable to list roles, %v", err)
	}

	for _, r := range list.Roles {
		if *r.RoleName == roleName {
			return r.Arn, nil
		}
	}

	create, err := iamSvc.CreateRole(ctx, &iam.CreateRoleInput{
		// AWSLambdaBasicExecutionRole
		AssumeRolePolicyDocument: aws.String(`{
			"Version": "2012-10-17",
			"Statement": [
				{
					"Effect": "Allow",
					"Principal":{
						"Service": "lambda.amazonaws.com"
					},
					"Action": "sts:AssumeRole"
				}
			]
		}`),
		RoleName: aws.String(roleName),
	})
	if err != nil {
		return nil, fmt.Errorf("unable to create role, %v", err)
	}

	for _, roleArn := range []string{
		//"arn:aws:iam::aws:policy/AWSLambda_FullAccess",
		//"arn:aws:iam::aws:policy/AmazonDynamoDBFullAccess",
		//"arn:aws:iam::aws:policy/AmazonS3FullAccess",
		//"arn:aws:iam::aws:policy/AmazonSESFullAccess",
		//"arn:aws:iam::aws:policy/AmazonSNSFullAccess",
		"arn:aws:iam::aws:policy/AmazonSQSFullAccess",
		"arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole",
	} {
		if _, err := iamSvc.AttachRolePolicy(
			ctx,
			&iam.AttachRolePolicyInput{
				PolicyArn: aws.String(roleArn),
				RoleName:  aws.String(roleName),
			},
		); err != nil {
			// Might as well try to add all the policies, even if one fails.
			fmt.Printf("unable to attach policy, %v\n", err)
		}
	}

	return create.Role.Arn, nil
}

func (l *lambdaDeployer) updateCode(ctx context.Context) error {
	envFilePath := fmt.Sprintf("%s/lambdas/cmd/%s/.env.json", l.projectDir, l.name)
	content, err := ioutil.ReadFile(envFilePath)
	if err != nil {
		return fmt.Errorf("unable to read env file, %v", err)
	}

	if _, err := l.svc.UpdateFunctionConfiguration(ctx, &lambda.UpdateFunctionConfigurationInput{
		FunctionName: &l.name,
		Environment: &types.Environment{Variables: map[string]string{
			"ENV": string(content),
		}},
	}); err != nil {
		return fmt.Errorf("unable to update config, %v", err)
	}

	zipContents, err := l.getZip()
	if err != nil {
		return fmt.Errorf("unable to build and get zip, %v", err)
	}

	if _, err := l.svc.UpdateFunctionCode(ctx, &lambda.UpdateFunctionCodeInput{
		Publish:      true,
		FunctionName: &l.name,
		ZipFile:      zipContents,
	}); err != nil {
		return fmt.Errorf("unable to update code, %v", err)
	}

	return nil
}

func (l *lambdaDeployer) configureSQS(ctx context.Context) error {
	if _, err := l.sqs.CreateQueue(
		ctx,
		&sqs.CreateQueueInput{
			QueueName: aws.String("watermeter-rpi.fifo"),
			Attributes: map[string]string{
				"FifoQueue": "true",
			},
			Tags: map[string]string{},
		},
	); err != nil {
		return fmt.Errorf("unable to create queue, %v", err)
	}
	return nil
}
