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

	if r.Spec.Strategy.SyncPolicy == "" {
		r.Spec.Strategy.SyncPolicy = IfAbsent
	}
	if r.Spec.Strategy.SyncDirection == "" {
		r.Spec.Strategy.SyncDirection = Cluster2Server
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
	if err := r.validateObjectRef(); err != nil {
		allErrs = append(allErrs, err)
	}
	if err := r.validateSyncStrategy(); err != nil {
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
	serverAddrEmpty := r.Spec.NacosServer.ServerAddr == nil || len(*r.Spec.NacosServer.ServerAddr) == 0
	endpoint := r.Spec.NacosServer.Endpoint == nil || len(*r.Spec.NacosServer.Endpoint) == 0
	if serverAddrEmpty && endpoint {
		return field.Required(field.NewPath("spec").Child("nacosServer"), "either ServerAddr or Endpoint should be set")
	}
	if len(r.Spec.NacosServer.Group) == 0 {
		return field.Required(field.NewPath("spec").Child("group"), "nacos group should be set")
	}
	if r.Spec.NacosServer.AuthRef == nil {
		return field.Required(field.NewPath("spec").Child("group"), "nacos auth reference should be set")
	} else {
		supportGVKs := []string{SecretGVK.String()}
		gvk := r.Spec.NacosServer.AuthRef.GroupVersionKind().String()
		if !stringsContains(supportGVKs, gvk) {
			return field.NotSupported(
				field.NewPath("spec").Child("nacosServer").Child("authRef").Child("group"),
				r.Spec.NacosServer.AuthRef,
				supportGVKs)
		}
	}
	if r.Spec.DataIds == nil || len(r.Spec.DataIds) == 0 {
		return field.Required(field.NewPath("spec").Child("dataIds"), "at least one dataId should be set")
	}
	return nil
}

func (r *DynamicConfiguration) validateObjectRef() *field.Error {
	if r.Spec.Strategy.SyncDirection != Cluster2Server {
		return nil
	}
	if r.Spec.ObjectRef == nil {
		return field.Required(field.NewPath("spec").Child("objectRef"), "ObjectRef should be set when SyncDirection is cluster2server")
	} else {
		supportGVKs := []string{ConfigMapGVK.String()}
		gvk := r.Spec.ObjectRef.GroupVersionKind().String()
		if !stringsContains(supportGVKs, gvk) {
			return field.NotSupported(
				field.NewPath("spec").Child("objectRef"),
				r.Spec.ObjectRef,
				supportGVKs)
		}
	}
	return nil
}

func (r *DynamicConfiguration) validateSyncStrategy() *field.Error {
	syncDirectionSupportList := []string{string(Cluster2Server), string(Server2Cluster)}
	if !stringsContains(syncDirectionSupportList, string(r.Spec.Strategy.SyncDirection)) {
		return field.NotSupported(
			field.NewPath("spec").Child("strategy").Child("syncDirection"),
			r.Spec.Strategy.SyncDirection,
			syncDirectionSupportList)
	}
	syncPolicySupportList := []string{string(Always), string(IfAbsent)}
	if !stringsContains(syncPolicySupportList, string(r.Spec.Strategy.SyncPolicy)) {
		return field.NotSupported(
			field.NewPath("spec").Child("strategy").Child("syncPolicy"),
			r.Spec.Strategy.SyncPolicy,
			syncPolicySupportList)
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
