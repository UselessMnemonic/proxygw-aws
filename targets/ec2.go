package targets

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/UselessMnemonic/proxygw/pkg/target"
)

const (
	defaultStartTimeout = 10 * time.Minute
	defaultStopTimeout  = 10 * time.Minute
	pollInterval        = 5 * time.Second
)

// EC2Handler starts and stops one or more EC2 instances for a proxygw target.
type EC2Handler struct {
	client       *ec2.Client
	instanceIDs  []string
	hibernate    bool
	startTimeout time.Duration
	stopTimeout  time.Duration
	logger       *slog.Logger
}

// Warm starts configured EC2 instances.
func (h *EC2Handler) Warm() error {
	ctx, cancel := context.WithTimeout(context.Background(), h.startTimeout)
	defer cancel()

	running, err := h.allInstancesInState(ctx, types.InstanceStateNameRunning)
	if err != nil {
		return err
	}
	if running {
		h.logger.Info("ec2 instances already running", "instance_ids", h.instanceIDs)
		return nil
	}

	h.logger.Info("starting ec2 instances", "instance_ids", h.instanceIDs)
	if _, err := h.client.StartInstances(ctx, &ec2.StartInstancesInput{InstanceIds: h.instanceIDs}); err != nil {
		h.logger.Error("ec2 start instances failed", "instance_ids", h.instanceIDs, "err", err)
		return err
	}

	if err := h.waitForState(ctx, types.InstanceStateNameRunning); err != nil {
		h.logger.Error("ec2 wait running failed", "instance_ids", h.instanceIDs, "err", err)
		return err
	}
	h.logger.Info("ec2 instances running", "instance_ids", h.instanceIDs)
	return nil
}

// Drain stops configured EC2 instances.
func (h *EC2Handler) Drain() error {
	ctx, cancel := context.WithTimeout(context.Background(), h.stopTimeout)
	defer cancel()

	stopped, err := h.allInstancesInState(ctx, types.InstanceStateNameStopped)
	if err != nil {
		return err
	}
	if stopped {
		h.logger.Info("ec2 instances already stopped", "instance_ids", h.instanceIDs)
		return nil
	}

	h.logger.Info("stopping ec2 instances", "instance_ids", h.instanceIDs, "hibernate", h.hibernate)
	if _, err := h.client.StopInstances(ctx, &ec2.StopInstancesInput{
		InstanceIds: h.instanceIDs,
		Hibernate:   aws.Bool(h.hibernate),
	}); err != nil {
		h.logger.Error("ec2 stop instances failed", "instance_ids", h.instanceIDs, "hibernate", h.hibernate, "err", err)
		return err
	}

	if err := h.waitForState(ctx, types.InstanceStateNameStopped); err != nil {
		h.logger.Error("ec2 wait stopped failed", "instance_ids", h.instanceIDs, "err", err)
		return err
	}
	h.logger.Info("ec2 instances stopped", "instance_ids", h.instanceIDs)
	return nil
}

// Close releases the target by stopping instances just like Drain.
func (h *EC2Handler) Close() error {
	return h.Drain()
}

func (h *EC2Handler) waitForState(ctx context.Context, state types.InstanceStateName) error {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		ready, err := h.allInstancesInState(ctx, state)
		if err != nil {
			return err
		}
		if ready {
			return nil
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for ec2 instances to reach %s: %w", state, ctx.Err())
		case <-ticker.C:
			h.logger.Debug("polling ec2 instances", "instance_ids", h.instanceIDs)
		}
	}
}

func (h *EC2Handler) allInstancesInState(ctx context.Context, state types.InstanceStateName) (bool, error) {
	out, err := h.client.DescribeInstances(ctx, h.describeInput())
	if err != nil {
		h.logger.Error("ec2 describe instances failed", "instance_ids", h.instanceIDs, "err", err)
		return false, err
	}

	found := make(map[string]types.InstanceStateName, len(h.instanceIDs))
	for _, reservation := range out.Reservations {
		for _, instance := range reservation.Instances {
			if instance.InstanceId == nil {
				continue
			}
			if instance.State == nil {
				found[*instance.InstanceId] = ""
				continue
			}
			found[*instance.InstanceId] = instance.State.Name
		}
	}

	for _, id := range h.instanceIDs {
		actual, ok := found[id]
		if !ok {
			return false, fmt.Errorf("ec2 describe instances did not return %q", id)
		}
		if actual != state {
			h.logger.Debug("ec2 instance state pending", "instance_id", id, "want", state, "got", actual)
			return false, nil
		}
	}
	return true, nil
}

func (h *EC2Handler) describeInput() *ec2.DescribeInstancesInput {
	return &ec2.DescribeInstancesInput{
		InstanceIds: h.instanceIDs,
	}
}

// NewEC2Handler creates an AWS EC2 target. Required options:
// instance_id or instance_ids. Optional options: region, profile, hibernate,
// start_timeout, stop_timeout.
func NewEC2Handler(name string, options map[string]any) (target.Handler, error) {
	instanceIDs, err := instanceIDsOption(options)
	if err != nil {
		return nil, err
	}

	region, err := stringOption(options, "region", "")
	if err != nil {
		return nil, err
	}
	profile, err := stringOption(options, "profile", "")
	if err != nil {
		return nil, err
	}
	hibernate, err := boolOption(options, "hibernate", false)
	if err != nil {
		return nil, err
	}
	startTimeout, err := durationOption(options, "start_timeout", defaultStartTimeout)
	if err != nil {
		return nil, err
	}
	stopTimeout, err := durationOption(options, "stop_timeout", defaultStopTimeout)
	if err != nil {
		return nil, err
	}

	loadOptions := make([]func(*config.LoadOptions) error, 0, 2)
	if region != "" {
		loadOptions = append(loadOptions, config.WithRegion(region))
	}
	if profile != "" {
		loadOptions = append(loadOptions, config.WithSharedConfigProfile(profile))
	}

	cfg, err := config.LoadDefaultConfig(context.Background(), loadOptions...)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	client := ec2.NewFromConfig(cfg)
	return &EC2Handler{
		client:       client,
		instanceIDs:  instanceIDs,
		hibernate:    hibernate,
		startTimeout: startTimeout,
		stopTimeout:  stopTimeout,
		logger:       slog.Default().With("handler", "ec2", "name", name, "region", cfg.Region),
	}, nil
}

func instanceIDsOption(options map[string]any) ([]string, error) {
	if options == nil {
		return nil, errors.New("aws ec2 target option instance_id or instance_ids is required")
	}

	if value, ok := options["instance_id"]; ok {
		id, err := stringValue(value, "instance_id")
		if err != nil {
			return nil, err
		}
		if id == "" {
			return nil, errors.New("aws ec2 target option instance_id must not be empty")
		}
		return []string{id}, nil
	}

	value, ok := options["instance_ids"]
	if !ok {
		return nil, errors.New("aws ec2 target option instance_id or instance_ids is required")
	}
	ids, err := stringSliceValue(value, "instance_ids")
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, errors.New("aws ec2 target option instance_ids must not be empty")
	}
	if slices.Contains(ids, "") {
		return nil, errors.New("aws ec2 target option instance_ids must not contain empty values")
	}
	return ids, nil
}

func stringOption(options map[string]any, key, fallback string) (string, error) {
	if options == nil {
		return fallback, nil
	}
	value, ok := options[key]
	if !ok {
		return fallback, nil
	}
	return stringValue(value, key)
}

func stringValue(value any, key string) (string, error) {
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("aws ec2 target option %s must be a string", key)
	}
	return text, nil
}

func stringSliceValue(value any, key string) ([]string, error) {
	switch ids := value.(type) {
	case []string:
		return ids, nil
	case []any:
		out := make([]string, 0, len(ids))
		for _, id := range ids {
			text, ok := id.(string)
			if !ok {
				return nil, fmt.Errorf("aws ec2 target option %s must contain only strings", key)
			}
			out = append(out, text)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("aws ec2 target option %s must be a string array", key)
	}
}

func boolOption(options map[string]any, key string, fallback bool) (bool, error) {
	if options == nil {
		return fallback, nil
	}
	value, ok := options[key]
	if !ok {
		return fallback, nil
	}
	switch v := value.(type) {
	case bool:
		return v, nil
	case string:
		switch strings.ToLower(v) {
		case "true":
			return true, nil
		case "false":
			return false, nil
		default:
			return false, fmt.Errorf("aws ec2 target option %s must be true or false", key)
		}
	default:
		return false, fmt.Errorf("aws ec2 target option %s must be a boolean", key)
	}
}

func durationOption(options map[string]any, key string, fallback time.Duration) (time.Duration, error) {
	if options == nil {
		return fallback, nil
	}
	value, ok := options[key]
	if !ok {
		return fallback, nil
	}
	switch v := value.(type) {
	case time.Duration:
		return v, nil
	case string:
		duration, err := time.ParseDuration(v)
		if err != nil {
			return 0, fmt.Errorf("aws ec2 target option %s: %w", key, err)
		}
		return duration, nil
	case int:
		return time.Duration(v) * time.Second, nil
	case int64:
		return time.Duration(v) * time.Second, nil
	case float64:
		return time.Duration(v * float64(time.Second)), nil
	default:
		return 0, fmt.Errorf("aws ec2 target option %s has unsupported type %s", key, reflect.TypeOf(value))
	}
}
