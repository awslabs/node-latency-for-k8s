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
	"time"

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

// Find searches the /var/log/messages log source based on the regexp string provided
func (m Source) Find(search string, firstOccurrence bool) (time.Time, error) {
	re, err := regexp.Compile(search)
	if err != nil {
		return time.Time{}, err
	}
	return m.logReader.Find(re, firstOccurrence)
}

func (m Source) String() string {
	return m.logReader.Path
}

// Name is the name of the source
func (m Source) Name() string {
	return Name
}
