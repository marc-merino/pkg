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
	"context"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"

	eventv1 "github.com/fluxcd/pkg/apis/event/v1"
	"github.com/fluxcd/pkg/runtime/cel"
)

// eventMetadataAnnotation holds a CEL expression over obj that returns extra event metadata.
const eventMetadataAnnotation = eventv1.Group + "/metadata"

var eventMetadataTimeout = time.Second

func computeEventMetadata(object runtime.Object, expr string, log logr.Logger) map[string]string {
	u, err := runtime.DefaultUnstructuredConverter.ToUnstructured(object)
	if err != nil {
		log.Error(err, "failed to convert object for the event metadata expression")
		return nil
	}

	e, err := cel.NewExpression(expr, cel.WithCompile(), cel.WithStructVariables("obj"))
	if err != nil {
		log.Error(err, "failed to compile the event metadata expression")
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), eventMetadataTimeout)
	defer cancel()
	result, err := e.EvaluateStringMap(ctx, map[string]any{"obj": u})
	if err != nil {
		log.Error(err, "failed to evaluate the event metadata expression")
		return nil
	}

	metadata := make(map[string]string, len(result))
	for k, v := range result {
		if k == "" || strings.Contains(k, "/") {
			log.Info("dropping event metadata entry with invalid key", "key", k)
			continue
		}
		metadata[eventv1.Group+"/"+k] = v
	}
	return metadata
}
