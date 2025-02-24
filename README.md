# Nacos Controller

This project includes a series of Kubernetes custom resources (CustomResourceDefinition) and their related controller implementations.
The current version defines CRDs as follows:

- DynamicConfiguration: Synchronization bridge between Nacos configuration and Kubernetes configuration.

[中文文档](./README_CN.md)

# Quick Start
## Deploy Nacos Controller
1. Install helm，see [document](https://helm.sh/docs/intro/install/)
2. Install Nacos Controller
```bash
git clone https://github.com/nacos-group/nacos-controller.git
cd nacos-controller/charts/nacos-controller

export KUBECONFIG=/path/to/your/kubeconfig/file
kubectl create ns nacos
helm install -n nacos nacos-controller .
```

## Configuration Synchronization Between Nacos and Kubernetes Clusters
Nacos Controller 2.0 supports bidirectional synchronization between Kubernetes cluster configurations and Nacos configurations. It can synchronize ConfigMaps and Secrets from specific Kubernetes namespaces to specified Nacos namespaces. Users can dynamically modify and manage Kubernetes cluster configurations through Nacos. The mapping relationship is as follows:

| ConfigMap/Secret | Nacos Config    |
|------------------|-----------------|
| Namespace        | User-specified namespace       |
| Name             | Group           |
| Key              | DataId          |
| Value            | Content         |

Currently supported synchronization strategies:
- Full synchronization: Automatically synchronizes all ConfigMaps and Secrets from specific Kubernetes namespaces to Nacos. Nacos Controller will auto-sync newly created ConfigMaps/Secrets
- Partial synchronization: Only synchronizes user-specified ConfigMaps and Secrets to Nacos

### Full Synchronization from K8s Namespace to Nacos
Create DynamicConfiguration YAML:
```yaml
apiVersion: nacos.io/v1
kind: DynamicConfiguration
metadata:
   name: dc-demo
spec:
   nacosServer:
      # endpoint: the address server of nacos server, conflict with serverAddr field, and higher priority than serverAddr field
      endpoint: <your-nacos-server-endpoint>
      # serverAddr: the address of nacos server, conflict with endpoint field
      serverAddr: <your-nacos-server-addr>
      # namespace: Target Nacos namespace
      namespace: <your-nacos-namespace-id>
      # authRef: Reference to the Secret that stores the Nacos client authentication information, supporting both username/password and Access Key/Secret Key. If the Nacos server does not have authentication enabled, this can be ignored.
      authRef:
         apiVersion: v1
         kind: Secret
         name: nacos-auth
   strategy:
      # scope: Synchronization strategy, where "full" indicates full synchronization and "partial" indicates partial synchronization.
      scope: full
      # Whether to synchronize configuration deletion operations
      syncDeletion: true
      # conflictPolicy: Synchronization conflict policy. "preferCluster" prioritizes Kubernetes cluster configuration, while "preferServer" prioritizes Nacos configuration.
      conflictPolicy: preferCluster
---
apiVersion: v1
kind: Secret
metadata:
    name: nacos-auth
data:
    accessKey: <base64 ak>
    secretKey: <base64 sk>
    username: <base64 your-nacos-username>
    password: <base64 your-nacos-password>
```
Run the command to deploy DynamicConfiguration to the namespace of the Kubernetes cluster that requires full synchronization:
```bash
kubectl apply -f dc-demo.yaml -n <namespace>
```
and the full synchronization of configurations will be achieved.

### Partial Synchronization from K8s Namespace to Nacos
Create a DynamicConfiguration YAML file. The main difference from full synchronization lies in the strategy section, and you need to specify the ConfigMap and Secret that require synchronization:
```yaml
apiVersion: nacos.io/v1
kind: DynamicConfiguration
metadata:
   name: dc-demo
spec:
   nacosServer:
      # endpoint: the address server of nacos server, conflict with serverAddr field, and higher priority than serverAddr field
      endpoint: <your-nacos-server-endpoint>
      # serverAddr: the address of nacos server, conflict with endpoint field
      serverAddr: <your-nacos-server-addr>
      # namespace: Target Nacos namespace
      namespace: <your-nacos-namespace-id>
      # authRef: Reference to the Secret that stores the Nacos client authentication information, supporting both username/password and Access Key/Secret Key. If the Nacos server does not have authentication enabled, this can be ignored.
      authRef:
         apiVersion: v1
         kind: Secret
         name: nacos-auth
   strategy:
      # scope: Synchronization strategy, where "full" indicates full synchronization and "partial" indicates partial synchronization.
      scope: partial
      # Whether to synchronize configuration deletion operations
      syncDeletion: true
      # conflictPolicy: Synchronization conflict policy. "preferCluster" prioritizes Kubernetes cluster configuration, while "preferServer" prioritizes Nacos configuration.
      conflictPolicy: preferCluster
   # The ConfigMap and Secret that need to be synchronized
   objectRefs:
      - apiVersion: v1
        kind: ConfigMap
        name: nacos-config-cm
      - apiVersion: v1
        kind: Secret
        name: nacos-config-secret
---
apiVersion: v1
kind: Secret
metadata:
    name: nacos-auth
data:
    accessKey: <base64 ak>
    secretKey: <base64 sk>
    username: <base64 your-nacos-username>
    password: <base64 your-nacos-password>
```
Run the command to deploy DynamicConfiguration to the namespace of the Kubernetes cluster that requires full synchronization:
```bash
kubectl apply -f dc-demo.yaml -n <namespace>
```
and the partial synchronization of configurations will be achieved.

### NacosServer Configuration
- endpoint: the address server of nacos server, conflict with serverAddr field, and higher priority than serverAddr field
- serverAddr: the address of nacos server, conflict with endpoint field
- namespace: the namespace id of nacos server
- group: the group of nacos server
- authRef: a reference of Object, which contains ak/sk of nacos server, currently only Secret is supported

```yaml
  nacosServer:
    endpoint: <your-nacos-server-endpoint>
    serverAddr: <your-nacos-server-addr>
    namespace: <your-nacos-namespace-id>
    group: <your-nacos-group>
    authRef:
      apiVersion: v1
      kind: Secret
      name: nacos-auth
```


## Contributors
Special thanks to the following individuals/teams for their contributions to this project:

- Alibaba Cloud [EDAS](https://www.aliyun.com/product/edas) team (project incubation source)