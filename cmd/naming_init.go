package main

import (
	"os"

	nacosiov1 "github.com/nacos-group/nacos-controller/api/v1"
	"github.com/nacos-group/nacos-controller/pkg/controller"
	"github.com/nacos-group/nacos-controller/pkg/nacos/auth"
	"github.com/nacos-group/nacos-controller/pkg/nacos/naming"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
)

func initNaming(mgr ctrl.Manager, clientSet *kubernetes.Clientset) {
	ncp, err := auth.NewDefaultNacosAuthProvider(mgr.GetClient()).GetNacosClientParams(&nacosiov1.DynamicConfiguration{})

	if err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ServiceDiscovery")
		os.Exit(1)
	}

	namespace := ncp.Namespace
	serverAddr := ncp.ServerAddr
	authInfo := ncp.AuthInfo
	accessKey := authInfo.AccessKey
	secretKey := authInfo.SecretKey

	opt := naming.NacosOptions{
		AccessKey:  accessKey,
		SecretKey:  secretKey,
		Namespace:  namespace,
		ServersIP:  []string{serverAddr},
		ServerPort: 8848,
	}

	if _, err := naming.NewNamingClient(opt); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ServiceDiscovery")
		os.Exit(1)
	} else {
		if err = (controller.NewServiceDiscoveryReconciler(mgr.GetClient(), clientSet)).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "ServiceDiscovery")
			os.Exit(1)
		}
	}
}
