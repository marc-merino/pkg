/*
Copyright 2026 The Flux authors

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

package events

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/fluxcd/pkg/runtime/events/eventstest"
	"github.com/fluxcd/pkg/runtime/testenv"
)

func TestEventRecorder_AnnotatedEventf_EventMetadata(t *testing.T) {
	for _, tt := range []struct {
		name             string
		object           runtime.Object
		inputAnnotations map[string]string
		expectedMetadata map[string]string
		absentMetadata   []string
	}{
		{
			name: "computed entries are added with the event group prefix",
			object: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "webapp",
					Namespace: "gitops-system",
					Labels:    map[string]string{"env": "production"},
					Annotations: map[string]string{
						eventMetadataAnnotation: `{"name": obj.metadata.name, "env": obj.metadata.labels["env"]}`,
					},
				},
			},
			expectedMetadata: map[string]string{
				"event.toolkit.fluxcd.io/name": "webapp",
				"event.toolkit.fluxcd.io/env":  "production",
			},
		},
		{
			name: "expression referencing obj.status",
			object: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "webapp",
					Namespace: "gitops-system",
					Annotations: map[string]string{
						eventMetadataAnnotation: `{"phase": obj.status.phase, "image": obj.status.containerStatuses[0].image}`,
					},
				},
				Status: corev1.PodStatus{
					Phase:             corev1.PodRunning,
					ContainerStatuses: []corev1.ContainerStatus{{Name: "app", Image: "ghcr.io/stefanprodan/podinfo:6.5.0"}},
				},
			},
			expectedMetadata: map[string]string{
				"event.toolkit.fluxcd.io/phase": "Running",
				"event.toolkit.fluxcd.io/image": "ghcr.io/stefanprodan/podinfo:6.5.0",
			},
		},
		{
			name: "object annotation wins over computed key",
			object: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "webapp",
					Namespace: "gitops-system",
					Labels:    map[string]string{"env": "production"},
					Annotations: map[string]string{
						eventMetadataAnnotation:       `{"env": obj.metadata.labels["env"], "name": obj.metadata.name}`,
						"event.toolkit.fluxcd.io/env": "explicit",
					},
				},
			},
			expectedMetadata: map[string]string{
				"event.toolkit.fluxcd.io/env":  "explicit",
				"event.toolkit.fluxcd.io/name": "webapp",
			},
		},
		{
			name: "input annotation wins over computed key",
			object: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "webapp",
					Namespace: "gitops-system",
					Annotations: map[string]string{
						eventMetadataAnnotation: `{"name": "computed"}`,
					},
				},
			},
			inputAnnotations: map[string]string{
				"event.toolkit.fluxcd.io/name": "input",
			},
			expectedMetadata: map[string]string{
				"event.toolkit.fluxcd.io/name": "input",
			},
		},
		{
			name: "invalid keys are dropped",
			object: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "webapp",
					Namespace: "gitops-system",
					Annotations: map[string]string{
						eventMetadataAnnotation: `{"a/b": "dropped", "": "dropped", "tag": "kept"}`,
					},
				},
			},
			expectedMetadata: map[string]string{
				"event.toolkit.fluxcd.io/tag": "kept",
			},
			absentMetadata: []string{
				"event.toolkit.fluxcd.io/a/b",
				"event.toolkit.fluxcd.io/",
			},
		},
		{
			name: "invalid expression still emits the event",
			object: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "webapp",
					Namespace: "gitops-system",
					Annotations: map[string]string{
						eventMetadataAnnotation: `{"env": obj.metadata.labels[`,
					},
				},
			},
			absentMetadata: []string{"event.toolkit.fluxcd.io/env"},
		},
		{
			name: "non-map result still emits the event",
			object: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "webapp",
					Namespace: "gitops-system",
					Annotations: map[string]string{
						eventMetadataAnnotation: `obj.metadata.name`,
					},
				},
			},
			absentMetadata: []string{"event.toolkit.fluxcd.io/name"},
		},
		{
			name: "non-string map value still emits the event",
			object: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "webapp",
					Namespace: "gitops-system",
					Annotations: map[string]string{
						eventMetadataAnnotation: `{"generation": obj.metadata.generation}`,
					},
				},
			},
			absentMetadata: []string{"event.toolkit.fluxcd.io/generation"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			sink := eventstest.NewSink(t)

			eventRecorder, err := NewRecorder(ctrl.Log, sink.URL(), "test-controller", WithManager(env))
			require.NoError(t, err)

			const msg = "sync object"

			eventRecorder.AnnotatedEventf(tt.object, nil, tt.inputAnnotations, corev1.EventTypeNormal, "sync", testAction, "%s", msg)
			require.Eventually(t, func() bool { return len(sink.Events()) == 1 }, time.Second, 10*time.Millisecond)

			payload := sink.Events()[0]
			require.Equal(t, msg, payload.Message)
			require.Equal(t, tt.expectedMetadata, payload.Metadata)
			require.NotContains(t, payload.Metadata, eventMetadataAnnotation)
			for _, k := range tt.absentMetadata {
				require.NotContains(t, payload.Metadata, k)
			}
		})
	}
}

func TestEventRecorder_AnnotatedEventf_EventMetadata_KubeEvent(t *testing.T) {
	sink := eventstest.NewSink(t)

	eventRecorder, err := NewRecorder(ctrl.Log, sink.URL(), "test-controller", WithManager(env))
	require.NoError(t, err)

	obj := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "webapp-computed-metadata",
			Namespace: "gitops-system",
			Labels:    map[string]string{"env": "production"},
			Annotations: map[string]string{
				eventMetadataAnnotation: `{"env": obj.metadata.labels["env"]}`,
			},
		},
	}
	require.NoError(t, env.Client.Create(ctx, obj))
	t.Cleanup(func() { _ = env.Client.Delete(ctx, obj) })

	eventRecorder.AnnotatedEventf(obj, nil, nil, corev1.EventTypeNormal, "sync", testAction, "sync %s", obj.Name)

	evs, err := testenv.WaitForEvents(ctx, env.Client, obj.Name, obj.Namespace,
		map[string]string{"event.toolkit.fluxcd.io/env": "production"}, 1, 10*time.Second)
	require.NoError(t, err)
	require.NotEmpty(t, evs)
	require.NotContains(t, evs[0].GetAnnotations(), eventMetadataAnnotation)

	require.Eventually(t, func() bool { return len(sink.Events()) == 1 }, time.Second, 10*time.Millisecond)
	require.Equal(t, map[string]string{"event.toolkit.fluxcd.io/env": "production"}, sink.Events()[0].Metadata)
	require.NotContains(t, sink.Events()[0].Metadata, eventMetadataAnnotation)
}

func TestEventRecorder_AnnotatedEventf_EventMetadata_Timeout(t *testing.T) {
	timeout := eventMetadataTimeout
	eventMetadataTimeout = time.Nanosecond
	t.Cleanup(func() { eventMetadataTimeout = timeout })

	data := make(map[string]string, 1000)
	for i := range 1000 {
		data[fmt.Sprintf("key%d", i)] = "value"
	}
	obj := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "webapp",
			Namespace: "gitops-system",
			Annotations: map[string]string{
				eventMetadataAnnotation: `{"count": string(obj.data.filter(k, k != "").size())}`,
			},
		},
		Data: data,
	}

	sink := eventstest.NewSink(t)
	eventRecorder, err := NewRecorder(ctrl.Log, sink.URL(), "test-controller", WithManager(env))
	require.NoError(t, err)

	eventRecorder.AnnotatedEventf(obj, nil, nil, corev1.EventTypeNormal, "sync", testAction, "%s", "sync object")
	require.Eventually(t, func() bool { return len(sink.Events()) == 1 }, time.Second, 10*time.Millisecond)
	require.NotContains(t, sink.Events()[0].Metadata, "event.toolkit.fluxcd.io/count")
	require.NotContains(t, sink.Events()[0].Metadata, eventMetadataAnnotation)
}
