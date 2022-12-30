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

// Package k8s is a latency timing source for K8s API events
package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/awslabs/node-latency-for-k8s/pkg/sources"

	"k8s.io/client-go/kubernetes"
)

var (
	Name = "K8s"
)

// Source is the K8s API http source
type Source struct {
	clientset    *kubernetes.Clientset
	nodeName     string
	podNamespace string
}

// New instantiates a new instance of the K8s API source
func New(clientset *kubernetes.Clientset, nodeName string, podNamespace string) *Source {
	return &Source{
		clientset:    clientset,
		nodeName:     nodeName,
		podNamespace: podNamespace,
	}
}

// ClearCache is a noop for the K8s API Source since it is an http source, not a log file
func (s Source) ClearCache() {}

// String is a human readable string of the source
func (s Source) String() string {
	return Name
}

// Name is the name of the source
func (s Source) Name() string {
	return Name
}

// FindPodCreationTime retrieves the Pod creation time
func (s *Source) FindPodCreationTime() sources.FindFunc {
	return func(_ sources.Source, _ []byte) ([]string, error) {
		ctx := context.Background()
		pods, err := s.clientset.CoreV1().Pods(s.podNamespace).List(ctx, v1.ListOptions{FieldSelector: fmt.Sprintf("spec.nodeName=%s", s.nodeName)})
		if err != nil {
			return nil, err
		}
		podMatches := lo.Map(pods.Items, func(p corev1.Pod, _ int) string {
			podBytes, err := json.Marshal(p)
			if err != nil {
				return ""
			}
			return string(podBytes)
		})
		return lo.Filter(podMatches, func(p string, _ int) bool { return p != "" }), nil
	}
}

// ParseTimeFor parses an event and returns the time
func (s *Source) ParseTimeFor(event []byte) (time.Time, error) {
	var pod *corev1.Pod
	if err := json.Unmarshal(event, &pod); err == nil && !pod.CreationTimestamp.IsZero() {
		return pod.CreationTimestamp.Time, nil
	}
	return time.Time{}, fmt.Errorf("unable to parse event")
}

// Find will use the Event's FindFunc and CommentFunc to search the source and return the result
func (s *Source) Find(event *sources.Event) ([]sources.FindResult, error) {
	k8sEvents, err := event.FindFn(s, nil)
	if err != nil {
		return nil, err
	}
	var results []sources.FindResult
	for _, k8sEvent := range k8sEvents {
		comment := ""
		if event.CommentFn != nil {
			comment = event.CommentFn(k8sEvent)
		}
		eventTime, err := s.ParseTimeFor([]byte(k8sEvent))
		results = append(results, sources.FindResult{
			Line:      k8sEvent,
			Timestamp: eventTime,
			Comment:   comment,
			Err:       err,
		})
	}
	return sources.SelectMatches(results, event.MatchSelector), nil
}
