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

// DynamicConfigurationSpec defines the desired state of DynamicConfiguration
type DynamicConfigurationSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	AdditionalConf *AdditionalConfiguration `json:"additionalConf,omitempty"`
	Strategy       SyncStrategy             `json:"strategy,omitempty"`
	NacosServer    NacosServerConfiguration `json:"nacosServer,omitempty"`
	ObjectRefs     []*v1.ObjectReference    `json:"objectRefs,omitempty"`
}

// DynamicConfigurationStatus defines the observed state of DynamicConfiguration
type DynamicConfigurationStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	Phase              string                  `json:"phase,omitempty"`
	Message            string                  `json:"message,omitempty"`
	ObservedGeneration int64                   `json:"observedGeneration,omitempty"`
	SyncStatuses       map[string][]SyncStatus `json:"syncStatuses,omitempty"`
	ListenConfigs      map[string][]string     `json:"listenConfigs,omitempty"`
	NacosServerStatus  NacosServerStatus       `json:"nacosServerStatus,omitempty"`
	SyncStrategyStatus SyncStrategy            `json:"syncStrategyStatus,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:resource:shortName=dc
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
//+kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// DynamicConfiguration is the Schema for the dynamicconfigurations API
type DynamicConfiguration struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DynamicConfigurationSpec   `json:"spec,omitempty"`
	Status DynamicConfigurationStatus `json:"status,omitempty"`
}

type AdditionalConfiguration struct {
	Labels     map[string]string `json:"labels,omitempty"`
	Properties map[string]string `json:"properties,omitempty"`
	Tags       map[string]string `json:"tags,omitempty"`
}

type SyncStrategy struct {
	SyncScope DynamicConfigurationSyncScope `json:"scope,omitempty"`
	//+kubebuilder:default=false
	SyncDeletion   bool                                   `json:"syncDeletion,omitempty"`
	ConflictPolicy DynamicConfigurationSyncConflictPolicy `json:"conflictPolicy,omitempty"`
}

type DynamicConfigurationSyncConflictPolicy string

const (
	PreferCluster DynamicConfigurationSyncConflictPolicy = "preferCluster"
	PreferServer  DynamicConfigurationSyncConflictPolicy = "preferServer"
)

type DynamicConfigurationSyncScope string

const (
	SyncScopePartial DynamicConfigurationSyncScope = "partial"
	SyncScopeFull    DynamicConfigurationSyncScope = "full"
)

type NacosServerConfiguration struct {
	Endpoint   string              `json:"endpoint,omitempty"`
	ServerAddr string              `json:"serverAddr,omitempty"`
	Namespace  string              `json:"namespace,omitempty"`
	AuthRef    *v1.ObjectReference `json:"authRef,omitempty"`
}

type NacosServerStatus struct {
	Endpoint   string `json:"endpoint,omitempty"`
	ServerAddr string `json:"serverAddr,omitempty"`
	Namespace  string `json:"namespace,omitempty"`
}

type SyncStatus struct {
	DataId       string      `json:"dataId,omitempty"`
	LastSyncTime metav1.Time `json:"lastSyncTime,omitempty"`
	LastSyncFrom string      `json:"lastSyncFrom,omitempty"`
	Md5          string      `json:"md5,omitempty"`
	Ready        bool        `json:"ready,omitempty"`
	Message      string      `json:"message,omitempty"`
}

//+kubebuilder:object:root=true

// DynamicConfigurationList contains a list of DynamicConfiguration
type DynamicConfigurationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DynamicConfiguration `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DynamicConfiguration{}, &DynamicConfigurationList{})
}
