package naming

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"strconv"
	"strings"
	"time"

	"github.com/nacos-group/nacos-sdk-go/v2/common/constant"
	"github.com/nacos-group/nacos-sdk-go/v2/util"
	"github.com/nacos-group/nacos-sdk-go/v2/vo"
)

type NacosHttpSdk struct {
	nacosAddresses []string
	nacosNamespace string
	accessKey      string
	secretKey      string
	httpClient     *http.Client
	isV3           bool
	userName       string
	password       string
}

const (
	defaultTimeout       = 10 * time.Second
	maxRetryAttempts     = 3
	retryInitialInterval = 500 * time.Millisecond
	v1ServicePath        = "/nacos/v1/ns/service"
	v1ClusterPath        = "/nacos/v1/ns/cluster"
	v3ServicePath        = "/nacos/v3/admin/ns/service"
	v3ClusterPath        = "/nacos/v3/admin/ns/cluster"
	v1InstanceListPath   = "/nacos/v1/ns/instance/list"
	v3InstanceListPath   = "/nacos/v3/admin/ns/instance/list"
)

type NacosInstanceListResponse struct {
	Name                     string `json:"name"`
	GroupName                string `json:"groupName"`
	Clusters                 string `json:"clusters"`
	CacheMillis              int    `json:"cacheMillis"`
	Hosts                    []Host `json:"hosts"`
	LastRefTime              int64  `json:"lastRefTime"`
	Checksum                 string `json:"checksum"`
	AllIPs                   bool   `json:"allIPs"`
	ReachProtectionThreshold bool   `json:"reachProtectionThreshold"`
	HitWeightProtection      bool   `json:"hitWeightProtection"`
	Dom                      string `json:"dom"`
	Valid                    bool   `json:"valid"`
}

type NewNacosInstanceListResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    []Host `json:"data"`
}

// Host represents a single host entry in the service discovery response.
type Host struct {
	InstanceId                string            `json:"instanceId"`
	IP                        string            `json:"ip"`
	Port                      int               `json:"port"`
	Weight                    float64           `json:"weight"`
	Healthy                   bool              `json:"healthy"`
	Enabled                   bool              `json:"enabled"`
	Ephemeral                 bool              `json:"ephemeral"`
	ClusterName               string            `json:"clusterName"`
	ServiceName               string            `json:"serviceName"`
	Metadata                  map[string]string `json:"metadata"`
	InstanceHeartBeatInterval int               `json:"instanceHeartBeatInterval"`
	InstanceHeartBeatTimeOut  int               `json:"instanceHeartBeatTimeOut"`
	IpDeleteTimeout           int               `json:"ipDeleteTimeout"`
	InstanceIdGenerator       string            `json:"instanceIdGenerator"`
}

func (n *NacosHttpSdk) executeRequest(method, apiPath string, params url.Values, body io.Reader) ([]byte, error) {
	baseURL := "http://" + n.nacosAddresses[rand.Intn(len(n.nacosAddresses))] + apiPath
	req, err := http.NewRequest(method, baseURL, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if len(params) > 0 {
		req.URL.RawQuery = params.Encode()
	}

	authHeaders := n.getSignHeadersForNaming(params)
	n.injectAuthHeaders(req, authHeaders)
	log.Log.Info("url: " + baseURL + ", params: " + req.URL.RawQuery + ", method: " + req.Method)

	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	resp, err := n.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bytes1, _ := io.ReadAll(resp.Body)
		if strings.Contains(string(bytes1), "service not found") {
			return bytes1, fmt.Errorf("service not found: %s", params.Get("serviceName"))
		}
		log.Log.Error(nil, fmt.Sprintf("Unexpected status %d, response: %s", resp.StatusCode, string(bytes1)))
		return bytes1, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

// return all instances of a service managed by nacos controller
func (n *NacosHttpSdk) GetAllInstances(param vo.SelectAllInstancesParam) ([]Address, error) {
	path := v1InstanceListPath

	if n.isV3 {
		path = v3InstanceListPath
	}

	apiUrl := "http://" + n.nacosAddresses[rand.Intn(len(n.nacosAddresses))] + path
	params := url.Values{}
	params.Add("namespaceId", n.nacosNamespace)
	params.Add("serviceName", param.ServiceName)
	params.Add("groupName", param.GroupName)
	params.Add("healthyOnly", "false")
	apiUrl += "?" + params.Encode()

	bs, err := n.executeRequest("GET", path, params, nil)
	if err != nil && bs == nil {
		log.Log.Error(err, "failed to get service providers from nacos, service name: "+param.ServiceName)
		return nil, err
	}

	result := string(bs)
	log.Log.Info("GetAllInstance, result: " + result)

	if strings.Contains(result, "not found") {
		log.Log.Info("GetAllInstance, service not found: " + param.ServiceName)
		return make([]Address, 0), nil
	}

	res := make([]Address, 0)

	if n.isV3 {
		svc := &NewNacosInstanceListResponse{}

		err = json.Unmarshal(bs, svc)
		if err != nil {
			log.Log.Error(err, "failed to unmarshal nacos response, url: "+apiUrl)
			return nil, err
		}
		for _, host := range svc.Data {
			if host.Metadata[NamingSyncedMark] != "true" {
				continue
			}
			res = append(res, Address{
				IP:   host.IP,
				Port: uint64(host.Port),
			})
		}
	} else {
		svc := &NacosInstanceListResponse{}
		err = json.Unmarshal(bs, svc)
		if err != nil {
			log.Log.Error(err, "failed to unmarshal nacos response, url: "+apiUrl)
			return nil, err
		}
		for _, host := range svc.Hosts {
			if host.Metadata[NamingSyncedMark] != "true" {
				continue
			}
			res = append(res, Address{
				IP:   host.IP,
				Port: uint64(host.Port),
			})
		}
	}
	return res, nil
}

type ServiceDetail struct {
	NamespaceId      string  `json:"namespaceId"`
	GroupName        string  `json:"groupName"`
	Name             string  `json:"name"`
	ProtectThreshold float64 `json:"protectThreshold"`
	Metadata         struct {
	} `json:"metadata"`
	Selector struct {
		Type        string `json:"type"`
		ContextType string `json:"contextType"`
	} `json:"selector"`
	Clusters []struct {
		Name          string `json:"name"`
		HealthChecker struct {
			Type                    string `json:"type"`
			TimeoutMs               int    `json:"timeoutMs"`
			InternalMs              int    `json:"internalMs"`
			HealthyCheckThreshold   int    `json:"healthyCheckThreshold"`
			UnhealthyCheckThreshold int    `json:"unhealthyCheckThreshold"`
		} `json:"healthChecker"`
		Metadata struct {
		} `json:"metadata"`
	} `json:"clusters"`
}

func NewNacosHttpSdk(servers []string, namespace, ak, sk string) *NacosHttpSdk {
	httpSdk := &NacosHttpSdk{
		nacosAddresses: servers,
		nacosNamespace: namespace,
		accessKey:      ak,
		secretKey:      sk,
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
		isV3: false,
	}
	httpSdk.getNacosVersion()
	return httpSdk
}

func (n *NacosHttpSdk) getNacosVersion() {
	urlV1 := fmt.Sprintf("%s://%s/nacos/v1/ns/service", "http", strings.Join(n.nacosAddresses, ","))
	req, err := http.NewRequest("GET", urlV1, nil)
	if err != nil {
		return
	}
	resp, err := n.httpClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusGone {
		n.isV3 = true
	}
}
func (n *NacosHttpSdk) getSignHeadersForNaming(params url.Values) map[string]string {
	result := map[string]string{}
	if n.accessKey == "" || n.secretKey == "" {
		result["userName"] = n.userName
		result["password"] = n.password
		return result
	}

	var signData string
	timeStamp := strconv.FormatInt(time.Now().UnixNano()/1e6, 10)
	if serviceName := params.Get("serviceName"); serviceName != "" {
		if groupName := params.Get("groupName"); groupName == "" {
			signData = timeStamp + constant.SERVICE_INFO_SPLITER + serviceName
		} else {
			signData = timeStamp + constant.SERVICE_INFO_SPLITER + util.GetGroupName(serviceName, groupName)
		}
	} else {
		signData = timeStamp
	}
	result["signature"] = n.signWithhmacSHA1Encrypt(signData, n.secretKey)
	result["Spas-AccessKey"] = n.accessKey
	result["ak"] = n.accessKey
	result["data"] = signData
	return result
}

func (n *NacosHttpSdk) signWithhmacSHA1Encrypt(encryptText, encryptKey string) string {
	//hmac ,use sha1
	key := []byte(encryptKey)
	mac := hmac.New(sha1.New, key)
	mac.Write([]byte(encryptText))

	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func (n *NacosHttpSdk) injectAuthHeaders(req *http.Request, headers map[string]string) {
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Encoding", "gzip, deflate, br")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	for k, v := range headers {
		req.Header.Set(k, v)
	}
}
func (n *NacosHttpSdk) CreatePersistService(key ServiceKey) bool {
	params := url.Values{}
	params.Add("namespaceId", n.nacosNamespace)
	params.Add("serviceName", key.ServiceName)
	params.Add("groupName", key.Group)

	path := v1ServicePath

	if n.isV3 {
		path = v3ServicePath
	}

	_, err := n.executeRequest("POST", path, params, bytes.NewBufferString(params.Encode()))
	if err != nil {
		log.Log.Error(err, "failed to create service: "+key.ServiceName)
		return false
	}
	return true
}
func (n *NacosHttpSdk) UpdateServiceHealthCheckTypeToNone(key ServiceKey) bool {
	var svc ServiceDetail
	var err error
	// 带重试的获取服务逻辑
	for attempt := 0; attempt < maxRetryAttempts; attempt++ {
		svc, err = n.getService(key.ServiceName, key.Group)
		if err == nil {
			break
		}
		if strings.Contains(err.Error(), "service not found") {
			if !n.CreatePersistService(key) {
				log.Log.Info("CreatePersistService fail")
				return false
			}
			time.Sleep(retryInitialInterval * time.Duration(1<<attempt))
			continue
		}
		log.Log.Error(err, "failed to get service: "+key.ServiceName)

		return false
	}

	for _, cluster := range svc.Clusters {
		if cluster.Name == "DEFAULT" {
			if cluster.HealthChecker.Type == "NONE" {
				return true
			}
		}
	}

	params := url.Values{}
	params.Add("namespaceId", n.nacosNamespace)
	params.Add("serviceName", key.ServiceName)
	params.Add("clusterName", "DEFAULT")
	params.Add("checkPort", "80")
	params.Add("useInstancePort4Check", "true")
	params.Add("healthChecker", "{\"type\":\"NONE\"}")

	path := v1ClusterPath

	if n.isV3 {
		path = v3ClusterPath
	}

	res, err := n.executeRequest("PUT", path, params, bytes.NewBufferString(params.Encode()))
	if err != nil {
		log.Log.Error(err, "failed to update health check, msg: "+string(res))
		return false
	}
	return true
}

func (n *NacosHttpSdk) getService(serviceName, groupName string) (ServiceDetail, error) {
	params := url.Values{}
	params.Add("namespaceId", n.nacosNamespace)
	params.Add("serviceName", serviceName)
	params.Add("groupName", groupName)
	if groupName == "" {
		groupName = NamingDefaultGroupName
	}

	path := v1ServicePath

	if n.isV3 {
		path = v3ServicePath
	}
	body, err := n.executeRequest("GET", path, params, nil)
	if err != nil {
		log.Log.Info("get service fail", string(body), err.Error())
		return ServiceDetail{}, fmt.Errorf("service request failed: %w", err)
	}

	var detail ServiceDetail
	if err := json.Unmarshal(body, &detail); err != nil {
		log.Log.Info("get service fail", string(body), err.Error())
		return ServiceDetail{}, fmt.Errorf("failed to unmarshal response: %w", err)
	}
	return detail, nil
}
