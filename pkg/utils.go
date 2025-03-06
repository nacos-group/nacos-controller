package pkg

import (
	"log"
	"os"

	"k8s.io/client-go/tools/clientcmd"
)

// Contains checks if the passed string is present in the given slice of strings.
func Contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// Remove deletes the passed string from the given slice of strings.
func Remove(list []string, s string) []string {
	for i, v := range list {
		if v == s {
			list = append(list[:i], list[i+1:]...)
		}
	}
	return list
}

var CurrentContext = "null"

func GetCurrentContext() {
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		kubeconfig = clientcmd.RecommendedHomeFile // 默认路径 ~/.kube/config
	}

	// 2. 使用 clientcmd 加载 kubeconfig 配置
	config, err := clientcmd.LoadFromFile(kubeconfig)
	if err != nil {
		log.Fatalf("Failed to load kubeconfig: %v", err)
		return
	}

	// 3. 获取 current-context
	CurrentContext = config.CurrentContext
}
