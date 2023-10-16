# Nacos Controller
本项目包含一系列Kubernetes自定义资源（CustomResourceDefinition）以及相关控制器实现。

当前版本定义CRD如下：
- DynamicConfiguration：Nacos配置与Kubernetes配置的同步桥梁

[English Document](./README.md)

## 快速开始
### 部署Nacos Controller
1. 安装helm，参考[文档](https://helm.sh/docs/intro/install/)
2. 安装Nacos Controller
```bash
git clone https://github.com/nacos-group/nacos-controller.git
cd nacos-controller/charts/nacos-controller

export KUBECONFIG=/你的K8s集群/访问凭证/文件路径
kubectl create ns nacos
helm install -n nacos nacos-controller .
```

### 从集群中同步配置到Nacos中
1. 一份Secret配置Nacos Server访问凭证，需包含ak和sk字段
2. 一份ConfigMap配置DataId和Content
3. 一份DynamicConfiguration定义微服务配置同步行为
   - spec.dataIds：定义哪些dataId需要被同步
   - spec.nacosServer：定义NacosServer信息
   - spec.strategy：定义同步策略
   - spec.objectRef：引用存放DataId和Content的载体
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

### 从Nacos中同步配置到集群中
1. 一份Secret配置Nacos Server访问凭证，需包含ak和sk字段
2. 一份ConfigMap用于存放DataId和Content（可选行为，不存在载体则自动创建）
3. 一份DynamicConfiguration定义微服务配置同步行为
    - spec.dataIds：定义哪些dataId需要被同步
    - spec.nacosServer：定义NacosServer信息
    - spec.strategy：定义同步策略
    - spec.objectRef：引用存放DataId和Content的载体（可选配置，留空则默认创建同名ConfigMap作为载体）
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

### NacosServer配置
字段说明：
- endpoint: nacos地址服务器，与serverAddr互斥，优先级高于serverAddr
- serverAddr: nacos地址，与endpoint互斥
- namespace: nacos空间ID
- group: nacos分组
- authRef: 引用存放Nacos AK/SK的资源，当前仅支持Secret
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


