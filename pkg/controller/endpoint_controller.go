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
	"encoding/json"
	"errors"
	"fmt"

	nacosiov1 "github.com/nacos-group/nacos-controller/api/v1"
	nacosv1 "github.com/nacos-group/nacos-controller/api/v1"
	"github.com/nacos-group/nacos-controller/pkg/nacos"
	"github.com/nacos-group/nacos-controller/pkg/nacos/auth"
	"github.com/nacos-group/nacos-controller/pkg/nacos/naming"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// EndpointReconciler reconciles a Endpoint object
type EndpointReconciler struct {
	Client client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=core,resources=endpoints,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=endpoints/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=core,resources=endpoints/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Endpoint object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.14.4/pkg/reconcile
func (r *EndpointReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)
	es := &corev1.Endpoints{}
	svcDeleted := false
	if err := r.Client.Get(ctx, req.NamespacedName, es); err != nil {
		if apierrors.IsNotFound(err) { //资源被删除，清除Nacos provider
			fmt.Println("Service deleted:", req.NamespacedName)
			svcDeleted = true
		} else {
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
	}

	if svcDeleted {
		es = &corev1.Endpoints{}
		es.Name = req.Name
		es.Namespace = req.Namespace
		es.Subsets = make([]corev1.EndpointSubset, 0)
	}

	log.Log.Info("Reconcile Endpoint", "endpoint", es.Name, "namespace", es.Namespace, "subset", es.Subsets)

	sdl := &nacosv1.ServiceDiscoveryList{}
	if err := r.Client.List(ctx, sdl, client.InNamespace(es.Namespace)); err != nil {
		return ctrl.Result{}, err
	}
	svcSds := make([]*nacosv1.ServiceDiscovery, 0)
	for _, sd := range sdl.Items {
		if len(sd.Spec.Services) == 0 || nacos.StringSliceContains(sd.Spec.Services, es.Name) {
			svcSds = append(svcSds, sd)
		}
	}

	if len(svcSds) == 0 {
		return ctrl.Result{}, nil
	}

	svc := &corev1.Service{}
	if err := r.Client.Get(ctx, client.ObjectKey{Namespace: es.Namespace, Name: es.Name}, svc); err != nil {
		if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, client.IgnoreNotFound(err)
		} else {
			svc = &corev1.Service{}
			svc.Name = es.Name
			svc.Namespace = es.Namespace
			svc.Annotations = make(map[string]string)
		}

	}

	serviceInfo, err := naming.GenerateServiceInfo(svc)

	if err != nil {
		return ctrl.Result{}, err
	}

	log.Log.Info("Sync instance to nacos", "serviceInfo", serviceInfo.ServiceName)
	for _, sd := range svcSds {

		if sd.Spec.NacosServer.ServerAddr != "" {
			log.Log.Info("Sync instance to nacos", "serverAddr", sd.Spec.NacosServer.ServerAddr)
			if err := r.SyncInstanceToNacos(ctx, es, serviceInfo, sd); err != nil {
				return ctrl.Result{}, err
			}
		}
	}

	return ctrl.Result{}, nil
}

// func (r *EndpointReconciler) UnRegisterService(ctx context.Context, es *corev1.Endpoints, serviceInfo nacosvclient.ServiceInfo, sd *nacosv1.ServiceDiscovery) error {
// 	addresses := naming.ConvertToAddresses(es, serviceInfo)
// 	log.Log.Info("Sync instance to nacos", "addresses", addresses)
// 	authProvider := auth.NewDefaultNacosAuthProvider(r.Client)
// 	log.Log.Info("Sync instance to nacos", "authProvider", authProvider)
// 	if nClient, err := nacosvclient.GetNacosNamingClientBuilder().BuildNamingClient(authProvider, sd); err == nil && nClient != nil {
// 		log.Log.Info("Sync instance to nacos1", "service", serviceInfo.ServiceName, "addresses", addresses)
// 		if !nClient.UnregisterService(serviceInfo) {
// 			marshal, err := json.Marshal(addresses)
// 			addressStr := ""
// 			if err == nil {
// 				addressStr = string(marshal)
// 			}
// 			return errors.New("Register service fail, serviceName: " + serviceInfo.ServiceName + ", addresses: " + addressStr)
// 		}

//		} else if err != nil {
//			log.Log.Info("failed to sync instance to nacos ", "error ", err)
//			return errors.New("Build nacos client fail,")
//		} else {
//			return errors.New("Build nacos client fail, NacosNamingClient is nil")
//		}
//		return nil
//	}
func (r *EndpointReconciler) SyncInstanceToNacos(ctx context.Context, es *corev1.Endpoints, serviceInfo naming.ServiceInfo, sd *nacosv1.ServiceDiscovery) error {
	addresses := naming.ConvertToAddresses(es, serviceInfo)
	log.Log.Info("Sync instance to nacos", "addresses", addresses)
	authProvider := auth.NewDefaultNacosAuthProvider(r.Client)
	log.Log.Info("Sync instance to nacos", "authProvider", authProvider)
	if nClient, err := naming.GetNacosNamingClientBuilder().BuildNamingClient(authProvider, sd); err == nil && nClient != nil {
		log.Log.Info("Sync instance to nacos1", "service", serviceInfo.ServiceName, "addresses", addresses)
		if !nClient.RegisterServiceInstances(serviceInfo, addresses) {
			marshal, err := json.Marshal(addresses)
			addressStr := ""
			if err == nil {
				addressStr = string(marshal)
			}
			return errors.New("Register service fail, serviceName: " + serviceInfo.ServiceName + ", addresses: " + addressStr)
		}

		status := nacosiov1.ServiceSyncStatus{
			ServiceName:  serviceInfo.ServiceName,
			GroupName:    serviceInfo.Group,
			Ready:        true,
			LastSyncTime: metav1.Now(),
		}

		if sd.Status.SyncStatuses == nil {
			sd.Status.SyncStatuses = make(map[string]*nacosiov1.ServiceSyncStatus)
		}

		sd.Status.SyncStatuses[serviceInfo.ServiceKey.String()] = &status

		if err := r.Client.Status().Update(ctx, sd); err != nil {
			return err
		}

	} else if err != nil {
		log.Log.Info("failed to sync instance to nacos ", "error ", err)
		return errors.New("Build nacos client fail")
	} else {
		return errors.New("Build nacos client fail, NacosNamingClient is nil")
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *EndpointReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Endpoints{}).
		Complete(r)
}
