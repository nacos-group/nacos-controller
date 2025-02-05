/*
Copyright 2023.

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

package v1

import (
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// log is for logging in this package.
var dynamicconfigurationlog = logf.Log.WithName("dynamicconfiguration-resource")
var (
	SecretGVK    = schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Secret"}
	ConfigMapGVK = schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}
)

func (r *DynamicConfiguration) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// TODO(user): EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

//+kubebuilder:webhook:path=/mutate-nacos-io-v1-dynamicconfiguration,mutating=true,failurePolicy=fail,sideEffects=None,groups=nacos.io,resources=dynamicconfigurations,verbs=create;update,versions=v1,name=mdynamicconfiguration.kb.io,admissionReviewVersions=v1

var _ webhook.Defaulter = &DynamicConfiguration{}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (r *DynamicConfiguration) Default() {
	dynamicconfigurationlog.Info("default", "name", r.Name)

	if r.Spec.Strategy.SyncScope == "" {
		if r.Spec.ObjectRefs != nil {
			r.Spec.Strategy.SyncScope = SyncScopePartial
		} else {
			r.Spec.Strategy.SyncScope = SyncScopeFull
		}
	}

	if r.Spec.Strategy.ConflictPolicy == "" {
		r.Spec.Strategy.ConflictPolicy = PreferCluster
	}
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
//+kubebuilder:webhook:path=/validate-nacos-io-v1-dynamicconfiguration,mutating=false,failurePolicy=fail,sideEffects=None,groups=nacos.io,resources=dynamicconfigurations,verbs=create;update,versions=v1,name=vdynamicconfiguration.kb.io,admissionReviewVersions=v1

var _ webhook.Validator = &DynamicConfiguration{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *DynamicConfiguration) ValidateCreate() (admission.Warnings, error) {
	dynamicconfigurationlog.Info("validate create", "name", r.Name)

	// TODO(user): fill in your validation logic upon object creation.
	return nil, r.validateDC()
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *DynamicConfiguration) ValidateUpdate(old runtime.Object) (admission.Warnings, error) {
	dynamicconfigurationlog.Info("validate update", "name", r.Name)

	// TODO(user): fill in your validation logic upon object update.
	return nil, r.validateDC()
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *DynamicConfiguration) ValidateDelete() (admission.Warnings, error) {
	dynamicconfigurationlog.Info("validate delete", "name", r.Name)

	// TODO(user): fill in your validation logic upon object deletion.
	return nil, nil
}

func (r *DynamicConfiguration) validateDC() error {
	var allErrs field.ErrorList
	if err := r.validateNacosServerConfiguration(); err != nil {
		allErrs = append(allErrs, err)
	}
	if err := r.validateSyncStrategy(); err != nil {
		allErrs = append(allErrs, err)
	}
	if err := r.validateObjectRefs(); err != nil {
		allErrs = append(allErrs, err)
	}
	if len(allErrs) == 0 {
		return nil
	}
	return errors.NewInvalid(
		schema.GroupKind{Group: "nacos.io", Kind: "DynamicConfiguration"},
		r.Name,
		allErrs)
}

func (r *DynamicConfiguration) validateNacosServerConfiguration() *field.Error {
	serverAddrEmpty := len(r.Spec.NacosServer.ServerAddr) == 0
	endpoint := len(r.Spec.NacosServer.Endpoint) == 0
	if serverAddrEmpty && endpoint {
		return field.Required(field.NewPath("spec").Child("nacosServer"), "either ServerAddr or Endpoint should be set")
	}
	if len(r.Spec.NacosServer.Namespace) == 0 {
		return field.Required(field.NewPath("spec").Child("nacosServer").Child("namespace"), "nacos namespace should be set")
	}
	if r.Spec.NacosServer.AuthRef != nil {
		supportGVKs := []string{SecretGVK.String()}
		gvk := r.Spec.NacosServer.AuthRef.GroupVersionKind().String()
		if !stringsContains(supportGVKs, gvk) {
			return field.NotSupported(
				field.NewPath("spec").Child("nacosServer").Child("authRef"),
				r.Spec.NacosServer.AuthRef,
				supportGVKs)
		}
	}
	return nil
}

func (r *DynamicConfiguration) validateObjectRefs() *field.Error {
	if r.Spec.Strategy.SyncScope == SyncScopeFull {
		if r.Spec.ObjectRefs != nil {
			return field.Invalid(field.NewPath("spec").Child("objectRefs"), r.Spec.ObjectRefs, "ObjectRefs should be empty when SyncStrategy is full")
		} else {
			return nil
		}
	}
	if r.Spec.ObjectRefs == nil {
		return field.Required(field.NewPath("spec").Child("objectRefs"), "ObjectRefs should be set when SyncStrategy is partial")
	} else {
		supportGVKs := []string{ConfigMapGVK.String(), SecretGVK.String()}
		for _, objRef := range r.Spec.ObjectRefs {
			gvk := objRef.GroupVersionKind().String()
			if !stringsContains(supportGVKs, gvk) {
				return field.NotSupported(
					field.NewPath("spec").Child("objectRefs"),
					objRef,
					supportGVKs)
			}
		}
	}
	return nil
}

func (r *DynamicConfiguration) validateSyncStrategy() *field.Error {
	syncScopeSupportList := []string{string(SyncScopePartial), string(SyncScopeFull)}
	if !stringsContains(syncScopeSupportList, string(r.Spec.Strategy.SyncScope)) {
		return field.NotSupported(
			field.NewPath("spec").Child("strategy").Child("syncDirection"),
			r.Spec.Strategy.SyncScope,
			syncScopeSupportList)
	}

	conflictPolicySupportList := []string{string(PreferCluster), string(PreferServer)}
	if !stringsContains(conflictPolicySupportList, string(r.Spec.Strategy.ConflictPolicy)) {
		return field.NotSupported(
			field.NewPath("spec").Child("strategy").Child("conflictPolicy"),
			r.Spec.Strategy.ConflictPolicy,
			conflictPolicySupportList)
	}
	return nil
}

func stringsContains(arr []string, item string) bool {
	if len(arr) == 0 {
		return false
	}
	for _, v := range arr {
		if v == item {
			return true
		}
	}
	return false
}
