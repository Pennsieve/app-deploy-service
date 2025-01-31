package pusher_config

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	pennsievePusher "github.com/pennsieve/pennsieve-go-core/pkg/models/pusher"
)

func Get(ctx context.Context, ssmClient *ssm.Client) (*pennsievePusher.Config, error) {
	getParameterOutput, err := ssmClient.GetParameter(ctx, &ssm.GetParameterInput{
		Name:           aws.String("/ops/pusher-config"),
		WithDecryption: aws.Bool(true),
	})
	if err != nil {
		return nil, fmt.Errorf("error getting Pusher config from SSM: %w", err)
	}
	configValue := *getParameterOutput.Parameter.Value
	var pusherConfig *pennsievePusher.Config
	if err := json.Unmarshal([]byte(configValue), &pusherConfig); err != nil {
		return nil, fmt.Errorf("error unmarshalling pusher config: %w", err)
	}
	return pusherConfig, nil
}
