/*
Copyright 2026 The KubeFleet Authors.

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

package fieldindexers

import (
	"context"

	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kubefleet-dev/kubefleet/pkg/utils/errors"
)

type fieldValueExtractor func(obj client.Object) ([]string, error)

func indexCompositeField(ctx context.Context,
	fieldIdxer client.FieldIndexer,
	obj client.Object,
	fieldName string, fieldValueExt fieldValueExtractor) error {
	if err := fieldIdxer.IndexField(ctx, obj, fieldName, func(rawObj client.Object) []string {
		fieldVals, extErr := fieldValueExt(rawObj)
		if extErr != nil {
			wrappedErr := errors.NewUnexpectedError(extErr, "failed to extract field value", "object", klog.KObj(rawObj))
			klog.ErrorS(wrappedErr, "failed to index field", errors.Args(wrappedErr)...)
			return nil
		}
		return fieldVals
	}); err != nil {
		wrappedErr := errors.NewUnexpectedError(err, "", "fieldName", fieldName, "object", klog.KObj(obj))
		klog.ErrorS(wrappedErr, "failed to index field", errors.Args(wrappedErr)...)
		return wrappedErr
	}
	return nil
}
