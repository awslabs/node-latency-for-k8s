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

// MessagesSource is the /var/log/messages log source
type MessagesSource struct {
	logReader *sources.LogReader
}

// New instantiates a new instance of messages source
func New(path string) *MessagesSource {
	return &MessagesSource{
		logReader: &sources.LogReader{
			Path:            path,
			Glob:            true,
			TimestampRegex:  TimestampFormat,
			TimestampLayout: TimestampLayout,
		},
	}
}

// ClearCache will clear the log reader cache
func (m MessagesSource) ClearCache() {
	m.logReader.ClearCache()
}

// Find searches the /var/log/messages log source based on the regexp string provided
func (m MessagesSource) Find(search string, firstOccurrence bool) (time.Time, error) {
	re, err := regexp.Compile(search)
	if err != nil {
		return time.Time{}, err
	}
	return m.logReader.Find(re, firstOccurrence)
}

func (m MessagesSource) String() string {
	return m.logReader.Path
}

// Name is the name of the source
func (m MessagesSource) Name() string {
	return Name
}
