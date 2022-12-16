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

// Source ius
type Source interface {
	// Find finds the string in the source using a source specific method (could be regex or HTTP path)
	// If no time.Time could be found an error is returned
	Find(search string, firstOccurrence bool) (time.Time, error)
	// Name is the source name identifier
	Name() string
	// ClearCache clears any cached source data
	ClearCache()
	// String is a human friendly version of the source, usually the log filepath
	String() string
}

// LogReader is a base Source helper that can Read file contents, cache, and support Glob file paths
// Other Sources can be built on-top of the LogSrc
type LogReader struct {
	Path            string
	Glob            bool
	TimestampRegex  *regexp.Regexp
	TimestampLayout string
	file            []byte
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
func (l *LogReader) Find(re *regexp.Regexp, firstOccurrence bool) (time.Time, error) {
	// Read the log file
	messages, err := l.Read()
	if err != nil {
		return time.Time{}, err
	}
	var line []byte
	if firstOccurrence {
		// Find all occurrences of the regex in the log file
		line = re.Find(messages)
		if len(line) == 0 {
			return time.Time{}, fmt.Errorf("no matches in %s for regex \"%s\"", l.Path, re.String())
		}
	} else {
		// Find all occurrences of the regex in the log file
		lines := re.FindAll(messages, -1)
		if len(lines) == 0 {
			return time.Time{}, fmt.Errorf("no matches in %s for regex \"%s\"", l.Path, re.String())
		}
		line = lines[len(lines)-1]
	}
	// Get Timestamp from line
	rawTS := l.TimestampRegex.FindString(string(line))
	if rawTS == "" {
		return time.Time{}, fmt.Errorf("unable to find timestamp on log line matching regex: \"%s\" \"%s\"", re.String(), line)
	}
	rawTS = spaceRE.ReplaceAllString(rawTS, " ")

	suffix := ""
	// Convert timestamp to a time.Time type
	if !strings.Contains(rawTS, fmt.Sprint(time.Now().Year())) {
		suffix = fmt.Sprintf(" %d", time.Now().Year())
	}
	ts, err := time.Parse(l.TimestampLayout, fmt.Sprintf("%s%s", rawTS, suffix))
	if err != nil {
		return time.Time{}, err
	}
	return ts, nil
}
