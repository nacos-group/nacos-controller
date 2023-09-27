# Nacos Controller

This project includes a series of Kubernetes custom resources (CustomResourceDefinition) and their related controller implementations.
The current version defines CRDs as follows:

- DynamicConfiguration: Synchronization bridge between Nacos configuration and Kubernetes configuration.

[中文文档](./README_CN.md)

## Quick Start
### Deploy Nacos Controller
// TBD

### Sync configuration from Kubernetes to Nacos Server
1. a Secret contains authorization information of nacos server, which is ak and sk
2. a ConfigMap contains dataIds and their content
3. a DynamicConfiguration, which defines the behaviour of synchronization
    - spec.dataIds：defines which dataIds should be synced
    - spec.nacosServer：defines the information of nacos server
    - spec.strategy：defines the strategy of synchronization
    - spec.objectRef：reference of object which contains dataIds and their content
```yaml
apiVersion: nacos.io/v1
kind: DynamicConfiguration
metadata:
    name: dc-demo-cluster2server
spec:
  dataIds:
  - data-id1.properties
  - data-id2.yml
  nacosServer:
    endpoint: <your-nacos-server-endpoint>
    namespace: <your-nacos-namespace-id>
    group: <your-nacos-group>
    authRef:
      apiVersion: v1
      kind: Secret
      name: nacos-auth
  strategy:
    syncPolicy: Always
    syncDirection: cluster2server
    syncDeletion: true
  objectRef:
    apiVersion: v1
    kind: ConfigMap
    name: nacos-config-cm

---
apiVersion: v1
kind: ConfigMap
metadata:
    name: nacos-config-cm
    namespace: default
data:
    data-id1.properties: |
      key=value
      key2=value2
    data-id2.yml: |
      app:
        name: test

---
apiVersion: v1
kind: Secret
metadata:
    name: nacos-auth
data:
    ak: <base64 ak>
    sk: <base64 sk>
```

### Sync configuration from Nacos Server to Kubernetes
1. a Secret contains authorization information of nacos server, which is ak and sk
2. a ConfigMap to store dataIds and their content (Optional, controller will create if this object doesn't exist)
3. a DynamicConfiguration, which defines the behaviour of synchronization
    - spec.dataIds：defines which dataIds should be synced
    - spec.nacosServer：defines the information of nacos server
    - spec.strategy：defines the strategy of synchronization
    - spec.objectRef：reference of object which contains dataIds and their content(Optional, if empty, controller will create a ConfigMap with same name as default object)

```yaml
apiVersion: nacos.io/v1
kind: DynamicConfiguration
metadata:
    name: dc-demo-server2cluster
spec:
  dataIds:
  - data-id1.properties
  - data-id2.yml
  nacosServer:
    endpoint: <your-nacos-server-endpoint>
    namespace: <your-nacos-namespace-id>
    group: <your-nacos-group>
    authRef:
      apiVersion: v1
      kind: Secret
      name: nacos-auth
  strategy:
    syncPolicy: Always
    syncDirection: server2cluster
    syncDeletion: true
---
apiVersion: v1
kind: Secret
metadata:
    name: nacos-auth
data:
    ak: <base64 ak>
    sk: <base64 sk>
```

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


