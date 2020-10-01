/*
Copyright 2020 The Crossplane Authors.

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

package resource

import (
	"context"

	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/crossplane/crossplane-runtime/apis/core/v1alpha1"
	"github.com/crossplane/crossplane-runtime/pkg/meta"
)

const (
	errMissingPCRef = "managed resource does not reference a ProviderConfig"
	errApplyPCU     = "cannot apply ProviderConfigUsage"
)

type errMissingRef struct{ error }

func (m errMissingRef) MissingReference() bool { return true }

// IsMissingReference returns true if an error indicates that a managed
// resource is missing a required reference..
func IsMissingReference(err error) bool {
	_, ok := err.(interface {
		MissingReference() bool
	})
	return ok
}

// A UsageTracker tracks usages of a ProviderConfig by creating or updating the
// appropriate ProviderConfigUsage.
type UsageTracker struct {
	c  Applicator
	of ProviderConfigUsage
}

// NewUsageTracker creates a UsageTracker.
func NewUsageTracker(c client.Client, of ProviderConfigUsage) *UsageTracker {
	return &UsageTracker{c: NewAPIUpdatingApplicator(c), of: of}
}

// Track that the supplied Managed resource is using the ProviderConfig it
// references by creating or updating a ProviderConfigUsage. Track should be
// called _before_ attempting to use the ProviderConfig. This ensures the
// managed resource's usage is updated if the managed resource is updated to
// reference a misconfigured ProviderConfig.
func (u *UsageTracker) Track(ctx context.Context, mg Managed) error {
	pcu := u.of.DeepCopyObject().(ProviderConfigUsage)
	gvk := mg.GetObjectKind().GroupVersionKind()
	ref := mg.GetProviderConfigReference()
	if ref == nil {
		return errMissingRef{errors.New(errMissingPCRef)}
	}

	pcu.SetName(string(mg.GetUID()))
	pcu.SetLabels(map[string]string{v1alpha1.LabelKeyProviderName: ref.Name})
	pcu.SetOwnerReferences([]metav1.OwnerReference{meta.AsController(meta.TypedReferenceTo(mg, gvk))})
	pcu.SetProviderConfigReference(v1alpha1.Reference{Name: ref.Name})
	pcu.SetResourceReference(v1alpha1.TypedReference{
		APIVersion: gvk.GroupVersion().String(),
		Kind:       gvk.Kind,
		Name:       mg.GetName(),
	})

	err := u.c.Apply(ctx, pcu,
		MustBeControllableBy(mg.GetUID()),
		AllowUpdateIf(func(current, _ runtime.Object) bool {
			return current.(ProviderConfigUsage).GetProviderConfigReference() != pcu.GetProviderConfigReference()
		}),
	)
	return errors.Wrap(Ignore(IsNotAllowed, err), errApplyPCU)
}
