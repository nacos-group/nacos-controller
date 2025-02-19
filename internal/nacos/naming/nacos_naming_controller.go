package nacos

import (
	nacosclient "github.com/nacos-group/nacos-controller/internal/nacos/client"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type SyncServiceController struct {
	cs           *kubernetes.Clientset
	namingClient nacosclient.NamingClient
}

type SyncServiceOptions struct {
	NamingClient nacosclient.NamingClient
}

func NewSyncServiceController(c client.Client, cs *kubernetes.Clientset, opt SyncServiceOptions) *SyncServiceController {
	if opt.NamingClient == nil {
		opt.NamingClient, _ = nacosclient.NewNamingClient(nacosclient.NacosOptions{})
	}
	return &SyncServiceController{
		cs:           cs,
		namingClient: opt.namingClient,
	}
}
