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

// Package awsnode is a latency timing source for the VPC CNI logs (aka aws-node DaemonSet)
package awsnode

import (
	"regexp"
	"sort"

	"github.com/awslabs/node-latency-for-k8s/pkg/sources"
)

var (
	Name            = "aws-node"
	DefaultPath     = "/var/log/pods/kube-system_aws-node-*/aws-node/*.log"
	TimestampFormat = regexp.MustCompile(`[0-9]{4}\-[0-9]{2}\-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]+Z`)
	TimestampLayout = "2006-01-02T15:04:05.999999999Z"
)

// Source is the aws-node / VPC CNI log source
type Source struct {
	logReader *sources.LogReader
}

// New instantiates a new instance of the AWSNode source
func New(path string, year int) *Source {
	return &Source{
		logReader: &sources.LogReader{
			Path:                 path,
			Glob:                 true,
			TimestampRegex:       TimestampFormat,
			TimestampLayout:      TimestampLayout,
			YearInstanceLaunched: year,
		},
	}
}

// ClearCache will clear the log reader cache
func (a Source) ClearCache() {
	a.logReader.ClearCache()
}

// String is a human readable string of the source, usually the log file path
func (a Source) String() string {
	return a.logReader.Path
}

// Name is the log source name
func (a Source) Name() string {
	return Name
}

// FindByRegex is a helper func that returns a FindFunc to search for a regex in a log source that can be used in an Event
func (a Source) FindByRegex(re *regexp.Regexp) sources.FindFunc {
	return func(s sources.Source, log []byte) ([]string, error) {
		return a.logReader.Find(re)
	}
}

// Find will use the Event's FindFunc and CommentFunc to search the log source and return the results based on the Event's matcher
func (a Source) Find(event *sources.Event) ([]sources.FindResult, error) {
	logBytes, err := a.logReader.Read()
	if err != nil {
		return nil, err
	}
	matchedLines, err := event.FindFn(a, logBytes)
	if err != nil {
		return nil, err
	}
	var results []sources.FindResult
	for _, line := range matchedLines {
		ts, err := a.logReader.ParseTimestamp(line)
		comment := ""
		if event.CommentFn != nil {
			comment = event.CommentFn(line)
		}
		results = append(results, sources.FindResult{
			Line:      line,
			Timestamp: ts,
			Err:       err,
			Comment:   comment,
		})
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Timestamp.UnixMicro() < results[j].Timestamp.UnixMicro()
	})
	return sources.SelectMatches(results, event.MatchSelector), nil
}
