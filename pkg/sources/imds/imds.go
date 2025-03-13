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

// Package imds is a latency timing source for the EC2 Instance Metadata Service (IMDS)
package imds

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/feature/ec2/imds"

	"github.com/awslabs/node-latency-for-k8s/pkg/sources"
)

var (
	Name             = "EC2 IMDS"
	DynamicDocPrefix = "/dynamic/instance-identity/document"
	PendingTime      = fmt.Sprintf("%s/%s", DynamicDocPrefix, "pendingTime")
)

// Source is the EC2 Instance Metadata Service (IMDS) http source
type Source struct {
	imds *imds.Client
}

// New instantiates a new instance of the IMDS source
func New(imdsClient *imds.Client) *Source {
	return &Source{
		imds: imdsClient,
	}
}

// ClearCache is a noop for the IMDS Source since it is an http source, not a log file
func (i Source) ClearCache() {}

// String is a human readable string of the source
func (i Source) String() string {
	return Name
}

// Name is the name of the source
func (i Source) Name() string {
	return Name
}

// FindByPath is a helper func that returns a FindFunc to query IMDS for a specific HTTP path that can be used in an Event
func (i Source) FindByPath(path string) sources.FindFunc {
	return func(_ sources.Source, _ []byte) ([]string, error) {
		result, err := i.GetMetadata(path)
		return []string{result}, err
	}
}

// Find will use the Event's FindFunc and CommentFunc to search the source and return the result
func (i Source) Find(event *sources.Event) ([]sources.FindResult, error) {
	timestamps, err := event.FindFn(i, nil)
	if err != nil {
		return nil, err
	}
	var results []sources.FindResult
	for _, tsStr := range timestamps {
		tsMicros, err := strconv.ParseInt(tsStr, 10, 64)
		comment := ""
		if event.CommentFn != nil {
			comment = event.CommentFn(tsStr)
		}
		results = append(results, sources.FindResult{
			Line:      tsStr,
			Timestamp: time.UnixMicro(tsMicros),
			Comment:   comment,
			Err:       err,
		})
	}
	return results, nil
}

// GetMetadata queries EC2 IMDS
func (i Source) GetMetadata(path string) (string, error) {
	ctx := context.TODO()
	identityDoc, err := i.imds.GetInstanceIdentityDocument(ctx, &imds.GetInstanceIdentityDocumentInput{})
	if err != nil {
		return "", fmt.Errorf("unable to retrieve instance-identity document: %w", err)
	}
	if path == PendingTime {
		return strconv.FormatInt(identityDoc.PendingTime.UnixMicro(), 10), nil
	}
	return "", fmt.Errorf("metadata for path \"%s\" is not available", path)
}
