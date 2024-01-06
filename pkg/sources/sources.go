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

package sources

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var (
	spaceRE = regexp.MustCompile(`\s+`)
)

// Source is an interface representing a source of events which have a time stamp or latency associated with them.
// Most often source is a log file or an API.
type Source interface {
	// Find finds the string in the source using a source specific method (could be regex or HTTP path)
	// If no time.Time could be found an error is returned
	Find(event *Event) ([]FindResult, error)
	// Name is the source name identifier
	Name() string
	// ClearCache clears any cached source data
	ClearCache()
	// String is a human friendly version of the source, usually the log filepath
	String() string
}

// FindResult is all data associated with a find including the raw Line data
type FindResult struct {
	Line      string
	Timestamp time.Time
	Comment   string
	Err       error
}

type FindFunc func(s Source, log []byte) ([]string, error)
type CommentFunc func(matchedLine string) string

// Event defines what is being timed from a specific source
type Event struct {
	Name          string      `json:"name"`
	Metric        string      `json:"metric"`
	MatchSelector string      `json:"matchSelector"`
	Terminal      bool        `json:"terminal"`
	SrcName       string      `json:"src"`
	Src           Source      `json:"-"`
	CommentFn     CommentFunc `json:"-"`
	FindFn        FindFunc    `json:"-"`
}

// Match Selector consts for an Event's MatchSelector
const (
	EventMatchSelectorFirst = "first"
	EventMatchSelectorLast  = "last"
	EventMatchSelectorAll   = "all"
)

// Timing is a specific instance of an Event timing
type Timing struct {
	Event     *Event        `json:"event"`
	Timestamp time.Time     `json:"timestamp"`
	T         time.Duration `json:"seconds"`
	Comment   string        `json:"comment"`
	Error     error         `json:"error"`
}

// SelectMaches will filter raw results based on the provided matchSelector
func SelectMatches(results []FindResult, matchSelector string) []FindResult {
	if len(results) == 0 {
		return nil
	}
	switch matchSelector {
	case EventMatchSelectorFirst:
		return []FindResult{results[0]}
	case EventMatchSelectorLast:
		return []FindResult{results[len(results)-1]}
	case EventMatchSelectorAll:
		return results
	}
	return results
}

// CommentMatchedLine is a helper func that returns a func that can be used as a CommentFunc in an Event
// The func will use the matched line as the comment
func CommentMatchedLine() func(matchedLine string) string {
	return func(matchedLine string) string {
		return matchedLine
	}
}

// LogReader is a base Source helper that can Read file contents, cache, and support Glob file paths
// Other Sources can be built on-top of the LogSrc
type LogReader struct {
	Path                 string
	Glob                 bool
	TimestampRegex       *regexp.Regexp
	TimestampLayout      string
	file                 []byte
	YearInstanceLaunched int
}

// ClearCache cleas the cached log
func (l *LogReader) ClearCache() {
	l.file = nil
}

// Read will open and read all the bytes of a log file into byte slice and then cache it
// Any further calls to Read() will use the cached byte slice.
// If the file is being updated and you need the updated contents,
// you'll need to instantiate a new LogSrc and call Read() again
func (l *LogReader) Read() ([]byte, error) {
	if l.file != nil {
		return l.file, nil
	}
	resolvedPath := l.Path
	if l.Glob {
		matches, err := filepath.Glob(l.Path)
		if err != nil || len(matches) == 0 {
			return nil, fmt.Errorf("unable to find log file %s: %w", l.Path, err)
		}
		// sort to find the oldest file for initial startup timings if the logs were rotated
		sort.Slice(matches, func(i, j int) bool {
			iFile, err := os.Open(matches[i])
			if err != nil {
				return matches[i] < matches[j]
			}
			defer iFile.Close()
			jFile, err := os.Open(matches[j])
			if err != nil {
				return matches[i] < matches[j]
			}
			defer jFile.Close()
			iStat, err := iFile.Stat()
			if err != nil {
				return matches[i] < matches[j]
			}
			jStat, err := jFile.Stat()
			if err != nil {
				return matches[i] < matches[j]
			}
			return iStat.ModTime().Unix() < jStat.ModTime().Unix()
		})
		resolvedPath = matches[0]
	}
	file, err := os.Open(resolvedPath)
	if err != nil {
		return nil, fmt.Errorf("unable to open log file %s: %w", resolvedPath, err)
	}
	defer file.Close()
	var reader io.Reader
	if strings.HasSuffix(resolvedPath, ".gz") {
		gzReader, err := gzip.NewReader(file)
		if err != nil {
			return nil, fmt.Errorf("unable to create gzip reader for file %s: %w", file.Name(), err)
		}
		defer gzReader.Close()
		reader = gzReader
	} else {
		reader = bufio.NewReader(file)
	}

	fileBytes, err := io.ReadAll(reader)
	if err != nil {
		return fileBytes, fmt.Errorf("unable to read file %s: %w", file.Name(), err)
	}
	l.file = fileBytes
	return fileBytes, nil
}

// Find searches for the passed in regexp from the log references in the LogReader
func (l *LogReader) Find(re *regexp.Regexp) ([]string, error) {
	// Read the log file
	messages, err := l.Read()
	if err != nil {
		return nil, err
	}
	// Find all occurrences of the regex in the log file
	lines := re.FindAll(messages, -1)
	if len(lines) == 0 {
		return nil, fmt.Errorf("no matches in %s for regex \"%s\"", l.Path, re.String())
	}
	var lineStrs []string
	for _, line := range lines {
		lineStrs = append(lineStrs, string(line))
	}
	return lineStrs, nil
}

// ParseTimestamp usese the configured timestamp regex to find a timestamp from the passed in log line and return as a time.Time
func (l *LogReader) ParseTimestamp(line string) (time.Time, error) {
	rawTS := l.TimestampRegex.FindString(line)
	if rawTS == "" {
		return time.Time{}, fmt.Errorf("unable to find timestamp on log line matching regex: \"%s\" \"%s\"", l.TimestampRegex.String(), line)
	}
	rawTS = spaceRE.ReplaceAllString(rawTS, " ")

	suffix := ""
	// Convert timestamp to a time.Time type
	if !strings.Contains(rawTS, fmt.Sprint(l.YearInstanceLaunched)) {
		suffix = fmt.Sprintf(" %d", l.YearInstanceLaunched)
	}
	ts, err := time.Parse(l.TimestampLayout, fmt.Sprintf("%s%s", rawTS, suffix))
	if err != nil {
		return time.Time{}, err
	}
	return ts, nil

}
