#!/bin/bash

# 检查是否提供了 nacos-server-addr 参数
if [ -z "$1" ]; then
  echo "Usage: $0 <nacos-server-addr> [nacos-namespace-id]"
  exit 1
fi

NACOS_SERVER_ADDR=$1
NACOS_NAMESPACE_ID=${2:-""}  # 如果第二个参数为空，则设置为默认值空字符串

# 直接生成 YAML 内容
GENERATED_CONTENT="apiVersion: nacos.io/v1
kind: DynamicConfiguration
metadata:
   name: dc-quickstart
spec:
   nacosServer:
      serverAddr: $NACOS_SERVER_ADDR
      namespace: ${NACOS_NAMESPACE_ID:-\"\"}
   strategy:
      scope: full
      syncDeletion: true
      conflictPolicy: preferCluster"

# 使用 kubectl 命令直接部署
echo "$GENERATED_CONTENT" | kubectl apply -f -

