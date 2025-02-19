package naming

import (
	"context"

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type SyncServiceController struct {
	K8sClientSet *kubernetes.Clientset
	NamingClient *NacosNamingClient
	K8sClient    client.Client
}

type SyncServiceOptions struct {
	NamingClient *NacosNamingClient
}

func NewSyncServiceController(c client.Client, cs *kubernetes.Clientset, opt SyncServiceOptions) *SyncServiceController {
	if nil == opt.NamingClient {
		nc, err := NewNamingClient(NacosOptions{})
		if err != nil {
			return nil
		}
		opt.NamingClient = nc
	}
	return &SyncServiceController{
		K8sClientSet: cs,
		NamingClient: opt.NamingClient,
		K8sClient:    c,
	}
}

func (scc *SyncServiceController) SyncService(ctx context.Context, obj client.Object) error {
	service := &v1.Service{}
	err := scc.K8sClient.Get(ctx, client.ObjectKeyFromObject(obj), service)
	if err != nil {
		return err
	}

	return nil
}
