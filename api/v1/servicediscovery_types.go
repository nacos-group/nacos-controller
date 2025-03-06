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
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ServiceDiscoverySpec defines the desired state of ServiceDiscovery
type ServiceDiscoverySpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	Services    []string                 `json:"services,omitempty"`
	SyncDirect  ServiceSyncDirection     `json:"syncDirect,omitempty"`
	NacosServer NacosServerConfiguration `json:"nacosServer,omitempty"`
	ObjectRef   *v1.ObjectReference      `json:"objectRef,omitempty"`
}

type ServiceSyncDirection string

const (
	ToNacos ServiceSyncDirection = "K8s2nacos"
	ToK8s   ServiceSyncDirection = "nacos2k8s"
)

// ServiceDiscoveryStatus defines the observed state of ServiceDiscovery
type ServiceDiscoveryStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	Message      string                        `json:"message,omitempty"`
	SyncStatuses map[string]*ServiceSyncStatus `json:"syncStatuses,omitempty"`
	ObjectRef    *v1.ObjectReference           `json:"objectRef,omitempty"`
}

type ServiceSyncStatus struct {
	ServiceName  string      `json:"serviceName,omitempty"`
	GroupName    string      `json:"groupName,omitempty"`
	LastSyncFrom string      `json:"lastSyncFrom,omitempty"`
	LastSyncTime metav1.Time `json:"lastSyncTime,omitempty"`
	Ready        bool        `json:"ready,omitempty"`
	Message      string      `json:"message,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// ServiceDiscovery is the Schema for the servicediscoveries API
type ServiceDiscovery struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ServiceDiscoverySpec   `json:"spec,omitempty"`
	Status ServiceDiscoveryStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// ServiceDiscoveryList contains a list of ServiceDiscovery
type ServiceDiscoveryList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []*ServiceDiscovery `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ServiceDiscovery{}, &ServiceDiscoveryList{})
}
