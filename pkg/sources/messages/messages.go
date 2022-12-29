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

// Package messages is a latency timing source for /var/log/messages
package messages

import (
	"regexp"
	"sort"

	"github.com/awslabs/node-latency-for-k8s/pkg/sources"
)

var (
	Name            = "Messages"
	DefaultPath     = "/var/log/messages*"
	TimestampFormat = regexp.MustCompile(`[A-Z][a-z]+[ ]+[0-9][0-9]? [0-9]{2}:[0-9]{2}:[0-9]{2}`)
	TimestampLayout = "Jan 2 15:04:05 2006"
)

// Source is the /var/log/messages log source
type Source struct {
	logReader *sources.LogReader
}

// New instantiates a new instance of messages source
func New(path string) *Source {
	return &Source{
		logReader: &sources.LogReader{
			Path:            path,
			Glob:            true,
			TimestampRegex:  TimestampFormat,
			TimestampLayout: TimestampLayout,
		},
	}
}

// ClearCache will clear the log reader cache
func (m Source) ClearCache() {
	m.logReader.ClearCache()
}

// String is a human readable string of the source, usually the log file path
func (m Source) String() string {
	return m.logReader.Path
}

// Name is the name of the source
func (m Source) Name() string {
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
