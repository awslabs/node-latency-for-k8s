// Package awsnode is a latency timing source for the VPC CNI logs (aka aws-node DaemonSet)
package awsnode

import (
	"regexp"
	"time"

	"github.com/awslabs/node-latency-for-k8s/pkg/sources"
)

var (
	Name            = "aws-node"
	DefaultPath     = "/var/log/pods/kube-system_aws-node-*/aws-node/*.log"
	TimestampFormat = regexp.MustCompile(`[0-9]{4}\-[0-9]{2}\-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]+Z`)
	TimestampLayout = "2006-01-02T15:04:05.999999999Z"
)

// AWSNodeSource is the aws-node / VPC CNI log source
type AWSNodeSource struct {
	logReader *sources.LogReader
}

// New instantiates a new instance of the AWSNode source
func New(path string) *AWSNodeSource {
	return &AWSNodeSource{
		logReader: &sources.LogReader{
			Path:            path,
			Glob:            true,
			TimestampRegex:  TimestampFormat,
			TimestampLayout: TimestampLayout,
		},
	}
}

// ClearCache will clear the log reader cache
func (a AWSNodeSource) ClearCache() {
	a.logReader.ClearCache()
}

// Find searches the aws-node log source based on the regexp string provided
func (a AWSNodeSource) Find(search string, firstOccurrence bool) (time.Time, error) {
	re, err := regexp.Compile(search)
	if err != nil {
		return time.Time{}, err
	}
	return a.logReader.Find(re, firstOccurrence)
}

func (a AWSNodeSource) String() string {
	return a.logReader.Path
}

// Name is the log source name
func (a AWSNodeSource) Name() string {
	return Name
}
