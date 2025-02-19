package naming

type Address struct {
	IP   string `json:"ip"`
	Port uint64 `json:"port"`
}

type NacosOptions struct {
	Namespace string

	// ServersIP are explicitly specified to be connected to nacos by client.
	ServersIP []string

	// ServerPort are explicitly specified to be used when the client connects to nacos.
	ServerPort uint64

	AccessKey string
	SecretKey string
}
