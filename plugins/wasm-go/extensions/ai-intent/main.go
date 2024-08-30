// File generated by hgctl. Modify as required.
// See:

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/alibaba/higress/plugins/wasm-go/pkg/wrapper"
	"github.com/higress-group/proxy-wasm-go-sdk/proxywasm"
	"github.com/higress-group/proxy-wasm-go-sdk/proxywasm/types"
	"github.com/tidwall/gjson"
)

const (
	DefaultPrompt  = "你是一个智能类别识别助手，负责根据用户提出的问题和预设的类别，确定问题属于哪个预设的类别，并给出相应的类别。用户提出的问题为:'%s',预设的类别为'%s'，直接返回一种具体类别，如果没有找到就返回'NotFound'。"
	defaultTimeout = 10 * 1000 // ms
)

func main() {
	wrapper.SetCtx(
		"ai-intent",
		wrapper.ParseConfigBy(parseConfig),
		wrapper.ProcessRequestHeadersBy(onHttpRequestHeaders),
		wrapper.ProcessRequestBodyBy(onHttpRequestBody),
		wrapper.ProcessResponseHeadersBy(onHttpResponseHeaders),
		wrapper.ProcessStreamingResponseBodyBy(onStreamingResponseBody),
		wrapper.ProcessResponseBodyBy(onHttpResponseBody),
	)
}

// @Name ai-intent
// @Category protocol
// @Phase AUTHN
// @Priority 1000
// @Title zh-CN AI intent
// @Description zh-CN 大模型意图识别
// @IconUrl
// @Version 0.1.0
//
// @Contact.name jose
// @Contact.url
// @Contact.email
//@Example

// scene:
//   category: "金融|电商|法律|Higress"
//   prompt:"你是一个智能类别识别助手，负责根据用户提出的问题和预设的类别，确定问题属于哪个预设的类别，并给出相应的类别。用户提出的问题为:%s,预设的类别为%s，直接返回一种具体类别，如果没有找到就返回'NotFound'。"
// 例："你是一个智能类别识别助手，负责根据用户提出的问题和预设的类别，确定问题属于哪个预设的类别，并给出相应的类别。用户提出的问题为:今天天气怎么样？,预设的类别为 ["金融","电商","法律"]，直接返回一种具体类别，如果没有找到就返回"NotFound"。"

type SceneInfo struct {
	Category string `require:"true" yaml:"category" json:"category"`
	Prompt   string `require:"false" yaml:"prompt" json:"prompt"`
	//解析category后的数组
	CategoryArr []string `yaml:"-" json:"-"`
}

type LLMInfo struct {
	ProxyServiceName string `require:"true" yaml:"proxyServiceName" json:"proxyServiceName"`
	ProxyUrl         string `require:"false" yaml:"proxyUrl" json:"proxyUrl"`
	ProxyModel       string `require:"false" yaml:"proxyModel" json:"proxyModel"`
	// @Title zh-CN 大模型服务端口
	// @Description zh-CN 服务端口
	ProxyPort int64 `required:"false" yaml:"proxyPort" json:"proxyPort"`
	// @Title zh-CN 大模型服务域名
	// @Description zh-CN 大模型服务域名
	ProxyDomain  string `required:"false" yaml:"proxyDomain" json:"proxyDomain"`
	ProxyTimeout uint32 `require:"false" yaml:"proxyTimeout" json:"proxyTimeout"`
	// @Title zh-CN 大模型服务的API_KEY
	// @Description zh-CN 大模型服务的API_KEY
	ProxyApiKey string             `require:"false" yaml:"proxyApiKey" json:"proxyApiKey"`
	ProxyClient wrapper.HttpClient `yaml:"-" json:"-"`
	// @Title zh-CN 大模型接口路径
	// @Description zh-CN 大模型接口路径
	ProxyPath string `yaml:"-" json:"-"`
}

type PluginConfig struct {
	// @Title zh-CN 意图相关配置
	// @Description zh-CN SceneInfo
	SceneInfo SceneInfo `required:"true" yaml:"scene" json:"scene"`
	// @Title zh-CN 大模型相关配置
	// @Description zh-CN LLMInfo
	LLMInfo LLMInfo `required:"true" yaml:"llm" json:"llm"`
	// @Title zh-CN  key 的来源
	// @Description zh-CN 使用的 key 的提取方式
	KeyFrom KVExtractor `required:"true" yaml:"keyFrom" json:"keyFrom"`
}

type KVExtractor struct {
	// @Title zh-CN 从请求 Body 中基于 [GJSON PATH](https://github.com/tidwall/gjson/blob/master/SYNTAX.md) 语法提取字符串
	RequestBody string `required:"false" yaml:"requestBody" json:"requestBody"`
	// @Title zh-CN 从响应 Body 中基于 [GJSON PATH](https://github.com/tidwall/gjson/blob/master/SYNTAX.md) 语法提取字符串
	ResponseBody string `required:"false" yaml:"responseBody" json:"responseBody"`
}

func parseConfig(json gjson.Result, c *PluginConfig, log wrapper.Log) error {
	log.Infof("config:%s", json.Raw)
	// init scene
	c.SceneInfo.Category = json.Get("scene.category").String()
	log.Infof("SceneInfo.Category:%s", c.SceneInfo.Category)
	if c.SceneInfo.Category == "" {
		return errors.New("scene.category must not by empty")
	}
	c.SceneInfo.CategoryArr = strings.Split(c.SceneInfo.Category, "|")
	if len(c.SceneInfo.CategoryArr) <= 0 {
		return errors.New("scene.category resolve exception, should use '|' split")
	}
	c.SceneInfo.Prompt = json.Get("scene.prompt").String()
	if c.SceneInfo.Prompt == "" {
		c.SceneInfo.Prompt = DefaultPrompt
	}
	log.Infof("SceneInfo.Prompt:%s", c.SceneInfo.Prompt)
	// init llmProxy
	log.Debug("Start to init proxyService's http client.")
	c.LLMInfo.ProxyServiceName = json.Get("llm.proxyServiceName").String()
	log.Infof("ProxyServiceName: %s", c.LLMInfo.ProxyServiceName)
	if c.LLMInfo.ProxyServiceName == "" {
		return errors.New("llm.proxyServiceName must not by empty")
	}
	c.LLMInfo.ProxyUrl = json.Get("llm.proxyUrl").String()
	log.Infof("c.LLMInfo.ProxyUrl:%s", c.LLMInfo.ProxyUrl)
	if c.LLMInfo.ProxyUrl == "" {
		return errors.New("llm.proxyUrl must not by empty")
	}
	//解析域名和path
	parsedURL, err := url.Parse(c.LLMInfo.ProxyUrl)
	if err != nil {
		return errors.New("llm.proxyUrl parsing error")
	}
	c.LLMInfo.ProxyPath = parsedURL.Path
	log.Infof("c.LLMInfo.ProxyPath:%s", c.LLMInfo.ProxyPath)
	c.LLMInfo.ProxyDomain = json.Get("llm.proxyDomain").String()
	//没有配置llm.proxyDomain时，则从proxyUrl中解析获取
	if c.LLMInfo.ProxyDomain == "" {
		hostName := parsedURL.Hostname()
		log.Infof("llm.proxyUrl.hostName:%s", hostName)
		if hostName != "" {
			c.LLMInfo.ProxyDomain = hostName
		}
	}
	log.Infof("c.LLMInfo.ProxyDomain:%s", c.LLMInfo.ProxyDomain)
	c.LLMInfo.ProxyPort = json.Get("llm.proxyPort").Int()
	// 没有配置llm.proxyPort时，则从proxyUrl中解析获取,如果解析的port为空，则http协议端口默认80，https端口默认443
	if c.LLMInfo.ProxyPort <= 0 {
		port := parsedURL.Port()
		log.Infof("llm.proxyUrl.port:%s", port)
		if port == "" {
			c.LLMInfo.ProxyPort = 80
			if parsedURL.Scheme == "https" {
				c.LLMInfo.ProxyPort = 443
			}
		} else {
			portNum, err := strconv.ParseInt(port, 10, 64)
			if err != nil {
				return errors.New("llm.proxyUrl.port parsing error")
			}
			c.LLMInfo.ProxyPort = portNum
		}
	}
	log.Infof("c.LLMInfo.ProxyPort:%s", c.LLMInfo.ProxyPort)
	c.LLMInfo.ProxyClient = wrapper.NewClusterClient(wrapper.FQDNCluster{
		FQDN: c.LLMInfo.ProxyServiceName,
		Port: c.LLMInfo.ProxyPort,
		Host: c.LLMInfo.ProxyDomain,
	})
	c.LLMInfo.ProxyModel = json.Get("llm.proxyModel").String()
	log.Infof("c.LLMInfo.ProxyModel:%s", c.LLMInfo.ProxyModel)
	if c.LLMInfo.ProxyModel == "" {
		c.LLMInfo.ProxyModel = "qwen-long"
	}
	c.LLMInfo.ProxyTimeout = uint32(json.Get("llm.proxyTimeout").Uint())
	log.Infof("c.LLMInfo.ProxyTimeout:%s", c.LLMInfo.ProxyTimeout)
	if c.LLMInfo.ProxyTimeout <= 0 {
		c.LLMInfo.ProxyTimeout = defaultTimeout
	}
	c.LLMInfo.ProxyApiKey = json.Get("llm.proxyApiKey").String()
	log.Infof("c.LLMInfo.ProxyApiKey:%s", c.LLMInfo.ProxyApiKey)
	c.KeyFrom.RequestBody = json.Get("keyFrom.requestBody").String()
	if c.KeyFrom.RequestBody == "" {
		c.KeyFrom.RequestBody = "messages.@reverse.0.content"
	}
	c.KeyFrom.ResponseBody = json.Get("keyFrom.responseBody").String()
	if c.KeyFrom.ResponseBody == "" {
		c.KeyFrom.ResponseBody = "choices.0.message.content"
	}
	log.Debug("Init ai intent's components successfully.")
	return nil
}

func onHttpRequestHeaders(ctx wrapper.HttpContext, config PluginConfig, log wrapper.Log) types.Action {
	log.Debug("start onHttpRequestHeaders function.")

	log.Debug("end onHttpRequestHeaders function.")
	return types.HeaderStopIteration
}

func onHttpRequestBody(ctx wrapper.HttpContext, config PluginConfig, body []byte, log wrapper.Log) types.Action {
	log.Debug("start onHttpRequestBody function.")
	bodyJson := gjson.ParseBytes(body)
	TempKey := strings.Trim(bodyJson.Get(config.KeyFrom.RequestBody).Raw, `"`)
	//原始问题
	originalQuestion, _ := zhToUnicode([]byte(TempKey))
	log.Infof("[onHttpRequestBody] originalQuestion is:  %s", string(originalQuestion))
	//prompt拼接,替换问题和预设的场景类别，参数占位替换
	promptStr := fmt.Sprintf(config.SceneInfo.Prompt, string(originalQuestion), config.SceneInfo.Category)
	log.Infof("[onHttpRequestBody] after prompt is:  %s", promptStr)
	proxyUrl, proxyRequestBody, proxyRequestHeader := generateProxyRequest(&config, []string{string(promptStr)}, log)
	log.Infof("[onHttpRequestBody] proxyUrl is:  %s", proxyUrl)
	log.Infof("[onHttpRequestBody] proxyRequestBody is:  %s", string(proxyRequestBody))
	//调用大模型 获取意向类型
	llmProxyErr := config.LLMInfo.ProxyClient.Post(
		proxyUrl,
		proxyRequestHeader,
		proxyRequestBody,
		func(statusCode int, responseHeaders http.Header, responseBody []byte) {
			log.Debug("Start llm.llmProxyClient func")
			log.Infof("llm.llmProxyClient statusCode is:%s", statusCode)
			log.Infof("llm.llmProxyClient intent responseBody is: %s", string(responseBody))
			if statusCode == 200 {
				proxyResponseBody, _ := proxyResponseHandler(responseBody, log)
				//大模型返回的识别到的意图类型
				if nil != proxyResponseBody && nil != proxyResponseBody.Choices && len(proxyResponseBody.Choices) > 0 {
					category := proxyResponseBody.Choices[0].Message.Content
					log.Infof("llmProxyClient intent response category is: %s", category)
					//验证返回结果是否为 定义的枚举值结果集合，判断返回结果是否在预设的类型中。
					for i := range config.SceneInfo.CategoryArr {
						//防止空格、空字符串
						if strings.TrimSpace(config.SceneInfo.CategoryArr[i]) == "" {
							continue
						}
						//2种判定条件，1.返回的category与该预设的场景完全一致 2.返回的category包含该预设的场景
						if config.SceneInfo.CategoryArr[i] == category || strings.Contains(category, config.SceneInfo.CategoryArr[i]) {
							// 把意图类型加入到Property中
							log.Debug("llmProxyClient intent category set to Property")
							proErr := proxywasm.SetProperty([]string{"intent_category"}, []byte(config.SceneInfo.CategoryArr[i]))
							if proErr != nil {
								log.Errorf("llmProxyClient proxywasm SetProperty error: %s", proErr.Error())
							}
							break
						}
					}
				}
			}
			_ = proxywasm.ResumeHttpRequest()
			return
		}, config.LLMInfo.ProxyTimeout)
	if llmProxyErr != nil {
		log.Errorf("llmProxy intent error: %s", llmProxyErr.Error())
		_ = proxywasm.ResumeHttpRequest()
	}
	log.Debug("end onHttpRequestHeaders function.")
	return types.ActionPause
}

func onHttpResponseHeaders(ctx wrapper.HttpContext, config PluginConfig, log wrapper.Log) types.Action {
	log.Debug("start onHttpResponseHeaders function.")

	log.Debug("end onHttpResponseHeaders function.")
	return types.ActionContinue
}

func onStreamingResponseBody(ctx wrapper.HttpContext, config PluginConfig, chunk []byte, isLastChunk bool, log wrapper.Log) []byte {
	log.Debug("start onStreamingResponseBody function.")

	log.Debug("end onStreamingResponseBody function.")
	return chunk
}

func onHttpResponseBody(ctx wrapper.HttpContext, config PluginConfig, body []byte, log wrapper.Log) types.Action {
	log.Debug("start onHttpResponseBody function.")

	log.Debug("end onHttpResponseBody function.")
	return types.ActionContinue
}

type ProxyRequest struct {
	Model    string                `json:"model"`
	Messages []ProxyRequestMessage `json:"messages"`
}

type ProxyRequestMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func generateProxyRequest(c *PluginConfig, texts []string, log wrapper.Log) (string, []byte, [][2]string) {
	url := c.LLMInfo.ProxyPath
	var userMessage ProxyRequestMessage
	userMessage.Role = "user"
	userMessage.Content = texts[0]
	var messages []ProxyRequestMessage
	messages = append(messages, userMessage)
	data := ProxyRequest{
		Model:    c.LLMInfo.ProxyModel,
		Messages: messages,
	}
	requestBody, err := json.Marshal(data)
	if err != nil {
		log.Errorf("[generateProxyRequest] Marshal json error:%s, data:%s.", err, data)
		return "", nil, nil
	}

	headers := [][2]string{
		{"Content-Type", "application/json"},
		{"Authorization", "Bearer " + c.LLMInfo.ProxyApiKey},
	}
	return url, requestBody, headers
}

func zhToUnicode(raw []byte) ([]byte, error) {
	str, err := strconv.Unquote(strings.Replace(strconv.Quote(string(raw)), `\\u`, `\u`, -1))
	if err != nil {
		return nil, err
	}
	return []byte(str), nil
}

type ProxyResponse struct {
	Status  int                          `json:"code"`
	Id      string                       `json:"id"`
	Choices []ProxyResponseOutputChoices `json:"choices"`
}

type ProxyResponseOutputChoices struct {
	FinishReason string                            `json:"finish_reason"`
	Message      ProxyResponseOutputChoicesMessage `json:"message"`
}

type ProxyResponseOutputChoicesMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func proxyResponseHandler(responseBody []byte, log wrapper.Log) (*ProxyResponse, error) {
	var response ProxyResponse
	err := json.Unmarshal(responseBody, &response)
	if err != nil {
		log.Errorf("[proxyResponseHandler]Unmarshal json error:%s", err)
		return nil, err
	}
	return &response, nil
}

func getProxyResponseByExtractor(c *PluginConfig, responseBody []byte, log wrapper.Log) string {
	bodyJson := gjson.ParseBytes(responseBody)
	responseContent := strings.Trim(bodyJson.Get(c.KeyFrom.ResponseBody).Raw, `"`)
	// llm返回的结果
	originalAnswer, _ := zhToUnicode([]byte(responseContent))
	return string(originalAnswer)
}
