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
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// SecretReconciler reconciles a Secret object
type SecretReconciler struct {
	client.Client
	controller *nacos.ConfigurationSyncController
	Scheme     *runtime.Scheme
}

func NewSecretReconciler(c client.Client, cs *kubernetes.Clientset, opt nacos.SyncConfigOptions, scheme *runtime.Scheme) *SecretReconciler {
	return &SecretReconciler{
		Client:     c,
		controller: nacos.NewConfigurationSyncController(c, cs, opt),
		Scheme:     scheme,
	}
}

// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=secrets/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core,resources=secrets/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Secret object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.1/pkg/reconcile
func (r *SecretReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx)
	defer func() {
		if r := recover(); r != nil {
			err := fmt.Errorf("panic occurred")
			l.Error(err, "panic", "req", req, "recover", r)
		}
	}()

	secret := corev1.Secret{}
	if err := r.Get(ctx, req.NamespacedName, &secret); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		l.Error(err, "get Secret error")
		return ctrl.Result{}, err
	}
	if secret.DeletionTimestamp != nil {
		return ctrl.Result{}, r.doFinalization(ctx, &secret)
	}
	if err := r.ensureFinalizer(ctx, &secret); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.controller.DoReconcile(ctx, &secret); err != nil {
		l.Error(err, "doReconcile error")
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, r.Update(ctx, &secret)
}

// SetupWithManager sets up the controller with the Manager.
func (r *SecretReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Secret{}).
		Named("secret").
		Complete(r)
}

func (r *SecretReconciler) doFinalization(ctx context.Context, secret *corev1.Secret) error {
	if !pkg.Contains(secret.GetFinalizers(), pkg.FinalizerName) {
		return nil
	}
	l := log.FromContext(ctx, "stage", "secret finalize")
	l.Info("doFinalization", "secret", secret)
	if err := r.controller.Finalize(ctx, secret); err != nil {
		l.Error(err, "secret finalize error")
		return err
	}
	l.Info("Remove finalizer", "secret", secret)
	secret.SetFinalizers(pkg.Remove(secret.GetFinalizers(), pkg.FinalizerName))
	if err := r.Update(ctx, secret); err != nil {
		l.Error(err, "secret finalize error")
		return err
	}
	return nil
}

func (r *SecretReconciler) ensureFinalizer(ctx context.Context, object client.Object) error {
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
