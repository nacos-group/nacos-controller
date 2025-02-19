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
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	nacosiov1 "github.com/nacos-group/nacos-controller/api/v1"
	"github.com/nacos-group/nacos-controller/internal/nacos"
	"github.com/nacos-group/nacos-controller/internal/nacos/auth"
	"github.com/nacos-group/nacos-controller/internal/nacos/naming"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// ServiceDiscoveryReconciler reconciles a ServiceDiscovery object
type ServiceDiscoveryReconciler struct {
	client.Client
	Scheme              *runtime.Scheme
	AuthProvider        *auth.NacosAuthProvider
	ServiceDiscoveryMap sync.Map
}

//+kubebuilder:rbac:groups=nacos.io.nacos.io,resources=servicediscoveries,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=nacos.io.nacos.io,resources=servicediscoveries/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=nacos.io.nacos.io,resources=servicediscoveries/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ServiceDiscovery object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.15.0/pkg/reconcile

func NewServiceDiscoveryReconciler(c client.Client, cs *kubernetes.Clientset) *ServiceDiscoveryReconciler {
	return &ServiceDiscoveryReconciler{
		Client: c,
		Scheme: c.Scheme(),
	}
}

func (r *ServiceDiscoveryReconciler) syncEndponits(ctx context.Context, endpointsList []v1.Endpoints, nClient *naming.NacosNamingClient, sd *nacosiov1.ServiceDiscovery) error {
	for _, endpoints := range endpointsList {
		svc := &v1.Service{}
		if err := r.Client.Get(ctx, client.ObjectKey{Namespace: endpoints.Namespace, Name: endpoints.Name}, svc); err != nil {
			return err
		}

		svcInfo, err := naming.GenerateServiceInfo(svc)
		if err != nil {
			return err
		}

		if !nClient.RegisterServiceInstances(svcInfo, naming.ConvertToAddresses(&endpoints, svcInfo)) {
			return errors.New("register service instances failed, service: " + svcInfo.ServiceName)
		}

		status := nacosiov1.ServiceSyncStatus{
			ServiceName:  svcInfo.ServiceName,
			GroupName:    svcInfo.Group,
			Ready:        true,
			LastSyncTime: metav1.Now(),
		}

		if sd.Status.SyncStatuses == nil {
			sd.Status.SyncStatuses = make(map[string]*nacosiov1.ServiceSyncStatus)
		}

		sd.Status.SyncStatuses[svcInfo.ServiceKey.String()] = &status

		if err := r.Client.Status().Update(ctx, sd); err != nil {
			return err
		}
	}
	return nil

}
func (r *ServiceDiscoveryReconciler) syncExistedService(ctx context.Context, sd *nacosiov1.ServiceDiscovery) error {
	fmt.Println("ServiceDiscovery Reconcile, syncExistedService start")
	toBeSynced := make([]v1.Endpoints, 0)
	authProvider := auth.NewDefaultNacosAuthProvider(r.Client)
	nClient, err := naming.GetNacosNamingClientBuilder().BuildNamingClient(authProvider, sd)

	if err != nil {
		return err
	}

	if len(sd.Spec.Services) > 0 {
		for _, svcName := range sd.Spec.Services {
			endpoints := v1.Endpoints{}
			if err := r.Client.Get(ctx, client.ObjectKey{Namespace: sd.Namespace, Name: svcName}, &endpoints); err != nil {
				return err
			}
			toBeSynced = append(toBeSynced, endpoints)
		}

	} else {
		endpointsList := &v1.EndpointsList{}
		if err := r.Client.List(ctx, endpointsList, &client.ListOptions{Namespace: sd.Namespace}); err != nil {
			return err
		}
		toBeSynced = append(toBeSynced, endpointsList.Items...)
	}

	if err := r.syncEndponits(ctx, toBeSynced, nClient, sd); err != nil {
		return err
	}

	fmt.Println("ServiceDiscovery Reconcile, syncExistedService success")

	return nil
}
func (r *ServiceDiscoveryReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)
	// TODO(user): your logic here
	log.Log.Info("ServiceDiscovery Reconcile", "req ", req.NamespacedName)
	sd := &nacosiov1.ServiceDiscovery{}
	fmt.Println("ServiceDiscovery Reconcile, namespace:", req.Namespace+", name: "+req.Name)
	err := r.Get(ctx, client.ObjectKey{Namespace: req.Namespace, Name: req.Name}, sd)
	if err != nil {
		// 如果资源不存在，可能是删除事件
		fmt.Println("ServiceDiscoveryReconciler: Service deleted:", req.NamespacedName, err)
		// 处理删除逻辑
		return ctrl.Result{}, nil
	}

	//新增资源，添加Finalizer，同步存量服务
	if !nacos.StringSliceContains(sd.Finalizers, "nacos.io/sd-finalizer") {
		// 确保 Finalizer
		if err := r.ensureFinalizer(ctx, sd); err != nil {
			return ctrl.Result{}, err
		}

		//同步存量服务
		return ctrl.Result{}, r.syncExistedService(ctx, sd)

	}

	// 资源被删除，清理服务
	if !sd.DeletionTimestamp.IsZero() {
		// 如果资源被删除，执行删除逻辑
		fmt.Println("ServiceDiscovery deleted:", req.NamespacedName)
		// 处理删除逻辑
		if err := r.doFinalization(ctx, sd); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	//处理更新逻辑
	endpointsList := make([]v1.Endpoints, 0)
	if len(sd.Spec.Services) > 0 {
		for _, svc := range sd.Spec.Services {
			endpoints := v1.Endpoints{}
			err := r.Client.Get(ctx, client.ObjectKey{Namespace: sd.Namespace, Name: svc}, &endpoints)

			if err != nil {
				if apierrors.IsNotFound(err) {
					continue
				} else {
					return ctrl.Result{}, err
				}
			}

			endpointsList = append(endpointsList, endpoints)
		}
	}

	authProvider := auth.NewDefaultNacosAuthProvider(r.Client)
	nClient, err := naming.GetNacosNamingClientBuilder().BuildNamingClient(authProvider, sd)

	if err != nil {
		return ctrl.Result{}, err
	}
	if err := r.syncEndponits(ctx, endpointsList, nClient, sd); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// 执行清理操作,删除nacos服务中被nacos controller管理的服务；
func (r *ServiceDiscoveryReconciler) doFinalization(ctx context.Context, sd *nacosiov1.ServiceDiscovery) error {
	fmt.Println("ServiceDiscovery Reconcile, doFinalization start")
	authProvider := auth.NewDefaultNacosAuthProvider(r.Client)
	nClient, err := naming.GetNacosNamingClientBuilder().BuildNamingClient(authProvider, sd)

	if err != nil {
		return err
	}

	var succ atomic.Bool
	succ.Store(true)

	for _, svcStatus := range sd.Status.SyncStatuses {
		if !svcStatus.Ready {
			continue
		}

		svcName := svcStatus.ServiceName
		if strings.Contains(svcName, "@@") {
			svcName = strings.Split(svcName, "@@")[0]
		}

		groupName := svcStatus.GroupName

		if groupName == "" {
			groupName = naming.NamingDefaultGroupName
		}

		log.Log.Info("Sync instance to nacos1", "service", svcName)
		if !nClient.UnregisterService(naming.ServiceInfo{
			ServiceKey: naming.ServiceKey{
				Group:       groupName,
				ServiceName: svcName,
			},
		}) {
			return errors.New("unregister service fail, serviceName: " + svcStatus.ServiceName)
		} else {
			log.Log.Info("Sync instance to nacos2", "service", svcStatus.ServiceName)
		}
	}

	if err := r.removeFinalizer(ctx, sd); err != nil {
		return err
	}

	return nil
}

func (r *ServiceDiscoveryReconciler) ensureFinalizer(ctx context.Context, obj client.Object) error {
	if !nacos.StringSliceContains(obj.GetFinalizers(), "nacos.io/sd-finalizer") {
		obj.SetFinalizers(append(obj.GetFinalizers(), "nacos.io/sd-finalizer"))
		if err := r.Client.Update(ctx, obj); err != nil {
			return err
		}
	}

	return nil
}
func (r *ServiceDiscoveryReconciler) removeFinalizer(ctx context.Context, obj client.Object) error {
	if nacos.StringSliceContains(obj.GetFinalizers(), "nacos.io/sd-finalizer") {
		finalizers := obj.GetFinalizers()
		for i, v := range finalizers {
			if v == "nacos.io/sd-finalizer" {
				finalizers = append(finalizers[:i], finalizers[i+1:]...)
			}
		}

		obj.SetFinalizers(finalizers)
		if err := r.Client.Update(ctx, obj); err != nil {
			return err
		}
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ServiceDiscoveryReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&nacosiov1.ServiceDiscovery{}).
		Complete(r)
}
