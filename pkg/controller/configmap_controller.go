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

package controller

import (
	"context"
	"fmt"

	"github.com/nacos-group/nacos-controller/pkg"
	"github.com/nacos-group/nacos-controller/pkg/nacos"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// ConfigMapReconciler reconciles a ConfigMap object
type ConfigMapReconciler struct {
	client.Client
	controller *nacos.ConfigurationSyncController
	Scheme     *runtime.Scheme
}

func NewConfigMapReconciler(c client.Client, cs *kubernetes.Clientset, opt nacos.SyncConfigOptions, scheme *runtime.Scheme) *ConfigMapReconciler {
	return &ConfigMapReconciler{
		Client:     c,
		controller: nacos.NewConfigurationSyncController(c, cs, opt),
		Scheme:     scheme,
	}
}

// +kubebuilder:rbac:groups=nacos.io,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=nacos.io,resources=configmaps/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=nacos.io,resources=configmaps/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ConfigMap object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.1/pkg/reconcile
func (r *ConfigMapReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx)
	defer func() {
		if r := recover(); r != nil {
			err := fmt.Errorf("panic occurred")
			l.Error(err, "panic", "req", req, "recover", r)
		}
	}()
	configMap := v1.ConfigMap{}
	if err := r.Get(ctx, req.NamespacedName, &configMap); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		l.Error(err, "get ConfigMap error")
		return ctrl.Result{}, err
	}
	if configMap.DeletionTimestamp != nil {
		return ctrl.Result{}, r.doFinalization(ctx, &configMap)
	}
	if err := r.ensureFinalizer(ctx, &configMap); err != nil {
		if errors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, err
	}
	if err := r.controller.DoReconcile(ctx, &configMap); err != nil {
		l.Error(err, "doReconcile error")
		return ctrl.Result{}, err
	}
	err := r.Update(ctx, &configMap)
	if err != nil {
		if errors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ConfigMapReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		// Uncomment the following line adding a pointer to an instance of the controlled resource as an argument
		For(&v1.ConfigMap{}).
		Named("configmap").
		Complete(r)
}

func (r *ConfigMapReconciler) doFinalization(ctx context.Context, configMap *v1.ConfigMap) error {
	if !pkg.Contains(configMap.GetFinalizers(), pkg.FinalizerName) {
		return nil
	}
	l := log.FromContext(ctx, "stage", "configMap finalize")
	l.Info("doFinalization", "configMap", configMap)
	if err := r.controller.Finalize(ctx, configMap); err != nil {
		l.Error(err, "configMap finalize error")
		return err
	}
	l.Info("Remove finalizer", "configMap", configMap)
	configMap.SetFinalizers(pkg.Remove(configMap.GetFinalizers(), pkg.FinalizerName))
	if err := r.Update(ctx, configMap); err != nil {
		l.Error(err, "remove finalizer error")
		return err
	}
	return nil
}

func (r *ConfigMapReconciler) ensureFinalizer(ctx context.Context, object client.Object) error {
	if controllerutil.ContainsFinalizer(object, pkg.FinalizerName) {
		return nil
	}
	l := log.FromContext(ctx)
	controllerutil.AddFinalizer(object, pkg.FinalizerName)
	if err := r.Update(ctx, object); err != nil {
		l.Error(err, "add ConfigMap finalizer error")
		return err
	}
	return nil
}
