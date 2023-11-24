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
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	runtimehandler "sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"strings"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	nacosiov1 "github.com/nacos-group/nacos-controller/api/v1"
)

const (
	PhaseSucceed string = "succeed"
	PhaseFailed  string = "failed"
)

const (
	FinalizerName string = "nacos.io/dc-finalizer"
)

// DynamicConfigurationReconciler reconciles a DynamicConfiguration object
type DynamicConfigurationReconciler struct {
	client.Client
	controller *nacos.SyncConfigurationController
}

func NewDynamicConfigurationReconciler(c client.Client, cs *kubernetes.Clientset, opt nacos.SyncConfigOptions) *DynamicConfigurationReconciler {
	return &DynamicConfigurationReconciler{
		Client:     c,
		controller: nacos.NewSyncConfigurationController(c, cs, opt),
	}
}

//+kubebuilder:rbac:groups=nacos.io,resources=dynamicconfigurations,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=nacos.io,resources=dynamicconfigurations/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=nacos.io,resources=dynamicconfigurations/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the DynamicConfiguration object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.15.0/pkg/reconcile
func (r *DynamicConfigurationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx)
	defer func() {
		if r := recover(); r != nil {
			err := fmt.Errorf("panic occurred")
			l.Error(err, "panic", "req", req, "recover", r)
		}
	}()
	dc := nacosiov1.DynamicConfiguration{}
	if err := r.Get(ctx, types.NamespacedName{
		Namespace: req.Namespace,
		Name:      req.Name,
	}, &dc); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		l.Error(err, "get DynamicConfiguration error")
		return ctrl.Result{}, err
	}
	if dc.DeletionTimestamp != nil {
		return ctrl.Result{}, r.doFinalization(ctx, &dc)
	}
	if err := r.ensureFinalizer(ctx, &dc); err != nil {
		return ctrl.Result{}, err
	}
	err := r.controller.SyncDynamicConfiguration(ctx, &dc)
	if err != nil {
		l.Error(err, "sync error")
		failedStatus(&dc, err.Error())
		_ = r.Status().Update(ctx, &dc)
		return ctrl.Result{}, err
	}
	updateStatus(&dc)
	return ctrl.Result{}, r.Status().Update(ctx, &dc)
}

func (r *DynamicConfigurationReconciler) ensureFinalizer(ctx context.Context, obj client.Object) error {
	if pkg.Contains(obj.GetFinalizers(), FinalizerName) {
		return nil
	}
	l := log.FromContext(ctx)
	l.Info("Add finalizer")
	obj.SetFinalizers(append(obj.GetFinalizers(), FinalizerName))
	if err := r.Update(ctx, obj); err != nil {
		l.Error(err, "update finalizer error")
		return err
	}
	return nil
}

func (r *DynamicConfigurationReconciler) doFinalization(ctx context.Context, dc *nacosiov1.DynamicConfiguration) error {
	if !pkg.Contains(dc.GetFinalizers(), FinalizerName) {
		return nil
	}
	l := log.FromContext(ctx)
	// 执行清理逻辑
	if err := r.controller.Finalize(ctx, dc); err != nil {
		failedStatus(dc, err.Error())
		if e := r.Status().Update(ctx, dc); e != nil {
			l.Error(e, "update status error")
		}
		return err
	}
	l.Info("Remove finalizer")
	dc.SetFinalizers(pkg.Remove(dc.GetFinalizers(), FinalizerName))
	if err := r.Update(ctx, dc); err != nil {
		l.Error(err, "remove finalizer error")
	}
	return nil
}

func (r *DynamicConfigurationReconciler) findDynamicConfiguration(ctx context.Context, obj client.Object) []reconcile.Request {
	labels := obj.GetLabels()
	v, ok := labels[pkg.ConfigMapLabel]
	if !ok {
		return []reconcile.Request{}
	}
	return []reconcile.Request{
		{
			NamespacedName: types.NamespacedName{
				Name:      v,
				Namespace: obj.GetNamespace(),
			},
		},
	}
}

func failedStatus(dc *nacosiov1.DynamicConfiguration, message string) {
	if dc == nil {
		return
	}
	dc.Status.Phase = PhaseFailed
	dc.Status.Message = message
	dc.Status.ObservedGeneration = dc.Generation
}

func updateStatus(dc *nacosiov1.DynamicConfiguration) {
	if dc == nil {
		return
	}
	dc.Status.ObservedGeneration = dc.Generation
	dc.Status.Phase = PhaseSucceed
	dc.Status.Message = ""

	var notReadyDataIds []string
	for _, syncStatus := range dc.Status.SyncStatuses {
		if !syncStatus.Ready {
			notReadyDataIds = append(notReadyDataIds, syncStatus.DataId)
		}
	}
	if len(notReadyDataIds) > 0 {
		dc.Status.Phase = PhaseFailed
		dc.Status.Message = fmt.Sprintf("not ready dataIds: %s", strings.Join(notReadyDataIds, ","))
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *DynamicConfigurationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&nacosiov1.DynamicConfiguration{}).
		WatchesMetadata(&v1.ConfigMap{},
			runtimehandler.EnqueueRequestsFromMapFunc(r.findDynamicConfiguration)).
		Complete(r)
}
