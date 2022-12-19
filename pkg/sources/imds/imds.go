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
	"time"

	"github.com/aws/aws-sdk-go-v2/feature/ec2/imds"
)

var (
	Name             = "EC2 IMDS"
	DynamicDocPrefix = "/dynamic/instance-identity/document"
	RequestedTime    = fmt.Sprintf("%s/%s", DynamicDocPrefix, "requestedTime")
	PendingTime      = fmt.Sprintf("%s/%s", DynamicDocPrefix, "pendingTime")
)

// Source is the EC2 Instance Metadata Service (IMDS) http source
type Source struct {
	imds *imds.Client
}

type ErrMetadata struct{}

func (e ErrMetadata) Error() string {
	return "metadata error"
}

// New instantiates a new instance of the IMDS source
func New(imdsClient *imds.Client) *Source {
	return &Source{
		imds: imdsClient,
	}
}

// ClearCache is a noop for the IMDS Source since it is an http source, not a log file
func (i Source) ClearCache() {}

// Find queries the EC2 Instance Metadata Service (IMDS) for the search path and expects to find a time.
// The only supported paths currently are "/dynamic/instance-identity/document/pendingTime" and "/dynamic/instance-identity/document/requestedTime"
func (i Source) Find(search string, firstOccurrence bool) (time.Time, error) {
	ctx := context.TODO()
	identityDoc, err := i.imds.GetInstanceIdentityDocument(ctx, &imds.GetInstanceIdentityDocumentInput{})
	if err != nil {
		return time.Time{}, fmt.Errorf("unable to retrieve instance-identity document: %v %w", err, ErrMetadata{})
	}
	if err != nil {
		return time.Time{}, err
	}
	switch search {
	case PendingTime:
		return identityDoc.PendingTime, nil
	case RequestedTime:
		// requestedTime doesn't actually exist, but this is approximately right that EC2 takes about 1 second
		// to handle the API request for an instances before it goes pending.
		return identityDoc.PendingTime.Add(-1 * time.Second), nil
	}
	return time.Time{}, fmt.Errorf("metadata for path \"%s\" is not available", search)
}

func (i Source) String() string {
	return Name
}

// Name is the name of the source
func (i Source) Name() string {
	return Name
}
