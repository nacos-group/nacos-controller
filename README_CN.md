# Nacos Controller
本项目包含一系列Kubernetes自定义资源（CustomResourceDefinition）以及相关控制器实现。

当前版本定义CRD如下：
- DynamicConfiguration：Nacos配置与Kubernetes配置的同步桥梁

[English Document](./README.md)

# 快速开始
## 部署Nacos Controller
1. 安装helm，参考[文档](https://helm.sh/docs/intro/install/)
2. 安装Nacos Controller
```bash
git clone https://github.com/nacos-group/nacos-controller.git
cd nacos-controller/charts/nacos-controller

export KUBECONFIG=/你的K8s集群/访问凭证/文件路径
kubectl create ns nacos
helm install -n nacos nacos-controller .
```
## Nacos和K8s集群配置同步
Nacos Controller 2.0 支持Kubernetes集群配置和Nacos 配置的双向同步，支持将Kubernetes集群特定命名空间下的ConfigMap以及Secret同步到Nacos指定命名空间下中。用户可以通过Nacos实现对于Kubenetes集群配置的动态修改和管理。Nacos配置和Kubernetes配置的映射关系如下表所示:

| ConfigMap/Secret | Nacos Config    |
|------------------|-----------------|
| Namespace        | 用户指定的命名空间       |
| Name             | Group           |
| Key              | DataId          |
| Value            | Content         |

目前主要支持两种配置同步的策略：
- 全量同步：Kubernetes集群特定命名空间下的所有ConfigMap以及Secret自动同步至Nacos,Nacos Controller会自动同步所有新建的ConfigMap和Secret
- 部分同步：只同步用户指定的ConfigMap和Secret至Nacos

### K8s集群命名空间配置全量同步Nacos
编写DynamicConfiguration yaml文件：
```yaml
apiVersion: nacos.io/v1
kind: DynamicConfiguration
metadata:
   name: dc-demo
spec:
   nacosServer:
      # endpoint: nacos地址服务器，与serverAddr互斥，优先级高于serverAddr，与serverAddr二选一即可
      endpoint: <your-nacos-server-endpoint>
      # serverAddr: nacos地址，与endpoint二选一即可
      serverAddr: <your-nacos-server-addr>
      # namespace: 用户指定的命名空间
      namespace: <your-nacos-namespace-id>
      # authRef: 引用存放Nacos 客户端鉴权信息的Secret，支持用户名/密码 和 AK/SK, Nacos服务端未开启鉴权可忽略
      authRef:
         apiVersion: v1
         kind: Secret
         name: nacos-auth
   strategy:
      # scope: 同步策略，full 表示全量同步，partial 表示部分同步
      scope: full
      # 是否同步配置删除操作
      syncDeletion: true
      # conflictPolicy: 同步冲突策略，preferCluster 表示初次同步内容冲突时以Kubernetes集群配置为准，preferServer 表示以Nacos配置为准
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
执行命令部署DynamicConfiguration到需要全量同步的Kubenetes集群命名空间下：
```bash
kubectl apply -f dc-demo.yaml -n <namespace>
```
即可实现配置的全量同步
### K8s集群命名空间配置部分同步Nacos
编写DynamicConfiguration yaml文件,和全量同步的区别主要在于strategy部分，并且要指定需要同步的ConfigMap和Secret：
```yaml
apiVersion: nacos.io/v1
kind: DynamicConfiguration
metadata:
   name: dc-demo
spec:
   nacosServer:
      # endpoint: nacos地址服务器，与serverAddr互斥，优先级高于serverAddr，与serverAddr二选一即可
      endpoint: <your-nacos-server-endpoint>
      # serverAddr: nacos地址，与endpoint二选一即可
      serverAddr: <your-nacos-server-addr>
      # namespace: 用户指定的命名空间
      namespace: <your-nacos-namespace-id>
      # authRef: 引用存放Nacos 客户端鉴权信息的Secret，支持用户名/密码 和 AK/SK, Nacos服务端未开启鉴权可忽略
      authRef:
         apiVersion: v1
         kind: Secret
         name: nacos-auth
   strategy:
      # scope: 同步策略，full 表示全量同步，partial 表示部分同步
      scope: partial
      # 是否同步配置删除操作
      syncDeletion: true
      # conflictPolicy: 同步冲突策略，preferCluster 表示初次同步内容冲突时以Kubernetes集群配置为准，preferServer 表示以Nacos配置为准
      conflictPolicy: preferCluster
   # 需要同步的ConfigMap和Secret
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
执行命令部署DynamicConfiguration到需要全量同步的Kubenetes集群命名空间下：
```bash
kubectl apply -f dc-demo.yaml -n <namespace>
```
即可实现配置的部分同步

### NacosServer配置
字段说明：
- endpoint: nacos地址服务器，与serverAddr互斥，优先级高于serverAddr
- serverAddr: nacos地址，与endpoint互斥
- namespace: nacos空间ID
- group: nacos分组
- authRef: 引用存放Nacos 客户端鉴权信息的资源，支持用户名/密码 和 AK/SK
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

## 贡献者
特别感谢以下人员/团队对本项目的贡献

- 阿里云[EDAS](https://www.aliyun.com/product/edas)团队（项目孵化来源）
