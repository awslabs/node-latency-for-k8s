/*
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package ec2 is a latency timing source for ec2 API events
package ec2

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/awslabs/node-latency-for-k8s/pkg/sources"
	"github.com/samber/lo"
)

var (
	Name            = "EC2"
	instanceIDRegex = regexp.MustCompile(`i-[0-9a-zA-Z]+`)
)

// Source is the EC2 Instance Metadata Service (IMDS) http source
type Source struct {
	ec2Client  *ec2.Client
	instanceID string
	fleetID    string
	nodeName   string
}

// New instantiates a new instance of the EC2 API source
func New(ec2Client *ec2.Client, instanceID string, nodeName string) *Source {
	return &Source{
		ec2Client:  ec2Client,
		instanceID: instanceID,
		nodeName:   nodeName,
	}
}

// ClearCache is a noop for the EC2 Source since it is an http source, not a log file
func (s Source) ClearCache() {}

// String is a human readable string of the source
func (s Source) String() string {
	return Name
}

// Name is the name of the source
func (s Source) Name() string {
	return Name
}

// FindFleetStart retrieves the Fleet request start time
func (s *Source) FindFleetStart() sources.FindFunc {
	return func(_ sources.Source, _ []byte) ([]string, error) {
		ctx := context.Background()
		var err error
		s.instanceID, err = s.getInstanceID(ctx)
		if err != nil {
			return nil, err
		}
		s.fleetID, err = s.getFleetID(ctx)
		if err != nil {
			return nil, err
		}
		fleetsOut, err := s.ec2Client.DescribeFleets(ctx, &ec2.DescribeFleetsInput{
			FleetIds: []string{s.fleetID},
		})
		if err != nil {
			return nil, err
		}
		if len(fleetsOut.Fleets) != 1 {
			return nil, fmt.Errorf("no fleet found for %s and fleet-id %s", s.instanceID, s.fleetID)
		}
		fleetBytes, err := json.Marshal(fleetsOut.Fleets[0])
		return []string{string(fleetBytes)}, err
	}
}

// getInstanceID retrieves the instance-id from cached values, node name, or DescribeInstances filtered by dns name
func (s Source) getInstanceID(ctx context.Context) (string, error) {
	if s.instanceID != "" {
		return s.instanceID, nil
	}
	if s.nodeName != "" {
		if instanceID := instanceIDRegex.FindString(s.nodeName); instanceID != "" {
			return instanceID, nil
		}
		instancesOut, err := s.ec2Client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
			Filters: []types.Filter{
				{
					Name:   lo.ToPtr("network-interface.private-dns-name"),
					Values: []string{s.nodeName},
				},
				{
					Name:   lo.ToPtr("instance-state-name"),
					Values: []string{"running"},
				},
			},
		})
		if err != nil {
			return "", err
		}
		if len(instancesOut.Reservations) != 1 && len(instancesOut.Reservations[0].Instances) != 1 {
			return "", fmt.Errorf("unable to discover instance-id from node-name: %s", s.nodeName)
		}
		return *instancesOut.Reservations[0].Instances[0].InstanceId, nil
	}
	return "", fmt.Errorf("unable to get instance ID")
}

// getFleetID retrieves the fleet-id using the instance-id to query for the aws fleet system tag
func (s Source) getFleetID(ctx context.Context) (string, error) {
	if s.fleetID != "" {
		return s.fleetID, nil
	}
	tagsOut, err := s.ec2Client.DescribeTags(ctx, &ec2.DescribeTagsInput{
		Filters: []types.Filter{
			{
				Name:   lo.ToPtr("resource-type"),
				Values: []string{"instance"},
			},
			{
				Name:   lo.ToPtr("resource-id"),
				Values: []string{s.instanceID},
			},
		},
	})
	if err != nil {
		return "", err
	}
	fleetTag, ok := lo.Find(tagsOut.Tags, func(t types.TagDescription) bool {
		return *t.Key == "aws:ec2:fleet-id"
	})
	if !ok {
		return "", fmt.Errorf("unable to find fleet tag for %s", s.instanceID)
	}
	return *fleetTag.Value, nil
}

func (s *Source) ParseTimeFor(event []byte) (time.Time, error) {
	var fleetData *types.FleetData
	if err := json.Unmarshal(event, &fleetData); err == nil && fleetData.CreateTime != nil {
		return *fleetData.CreateTime, nil
	}
	return time.Time{}, fmt.Errorf("unable to parse event")
}

// Find will use the Event's FindFunc and CommentFunc to search the source and return the result
func (s *Source) Find(event *sources.Event) ([]sources.FindResult, error) {
	ec2Events, err := event.FindFn(s, nil)
	if err != nil {
		return nil, err
	}
	var results []sources.FindResult
	for _, ec2Event := range ec2Events {
		comment := ""
		if event.CommentFn != nil {
			comment = event.CommentFn(ec2Event)
		}
		eventTime, err := s.ParseTimeFor([]byte(ec2Event))
		results = append(results, sources.FindResult{
			Line:      ec2Event,
			Timestamp: eventTime,
			Comment:   comment,
			Err:       err,
		})
	}
	return results, nil
}
