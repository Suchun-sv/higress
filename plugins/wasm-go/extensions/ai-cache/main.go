package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/alibaba/higress/plugins/wasm-go/pkg/wrapper"
	"github.com/google/uuid"
	"github.com/higress-group/proxy-wasm-go-sdk/proxywasm"
	"github.com/higress-group/proxy-wasm-go-sdk/proxywasm/types"
	"github.com/tidwall/gjson"
	"github.com/tidwall/resp"
)

const (
	CacheKeyContextKey       = "cacheKey"
	CacheContentContextKey   = "cacheContent"
	PartialMessageContextKey = "partialMessage"
	ToolCallsContextKey      = "toolCalls"
	StreamContextKey         = "stream"
	CacheKeyPrefix           = "higressAiCache"
	DefaultCacheKeyPrefix    = "higressAiCache"
	QueryEmbeddingKey        = "queryEmbedding"
)

func main() {
	wrapper.SetCtx(
		"ai-cache",
		wrapper.ParseConfigBy(parseConfig),
		wrapper.ProcessRequestHeadersBy(onHttpRequestHeaders),
		wrapper.ProcessRequestBodyBy(onHttpRequestBody),
		wrapper.ProcessResponseHeadersBy(onHttpResponseHeaders),
		wrapper.ProcessStreamingResponseBodyBy(onHttpResponseBody),
	)
}

// @Name ai-cache
// @Category protocol
// @Phase AUTHN
// @Priority 10
// @Title zh-CN AI Cache
// @Description zh-CN 大模型结果缓存
// @IconUrl
// @Version 0.1.0
//
//
// @Contact.name suchunsv
// @Contact.url
// @Contact.email

type RedisInfo struct {
	// @Title zh-CN redis 服务名称
	// @Description zh-CN 带服务类型的完整 FQDN 名称，例如 my-redis.dns、redis.my-ns.svc.cluster.local
	ServiceName string `required:"true" yaml:"serviceName" json:"serviceName"`
	// @Title zh-CN redis 服务端口
	// @Description zh-CN 默认值为6379
	ServicePort int `required:"false" yaml:"servicePort" json:"servicePort"`
	// @Title zh-CN 用户名
	// @Description zh-CN 登陆 redis 的用户名，非必填
	Username string `required:"false" yaml:"username" json:"username"`
	// @Title zh-CN 密码
	// @Description zh-CN 登陆 redis 的密码，非必填，可以只填密码
	Password string `required:"false" yaml:"password" json:"password"`
	// @Title zh-CN 请求超时
	// @Description zh-CN 请求 redis 的超时时间，单位为毫秒。默认值是1000，即1秒
	Timeout int `required:"false" yaml:"timeout" json:"timeout"`
}

type DashVectorInfo struct {
	DashScopeServiceName  string             `require:"true" yaml:"DashScopeServiceName" jaon:"DashScopeServiceName"`
	DashScopeKey          string             `require:"true" yaml:"DashScopeKey" jaon:"DashScopeKey"`
	DashVectorServiceName string             `require:"true" yaml:"DashVectorServiceName" jaon:"DashVectorServiceName"`
	DashVectorKey         string             `require:"true" yaml:"DashVectorKey" jaon:"DashVectorKey"`
	DashVectorAuthApiEnd  string             `require:"true" yaml:"DashVectorEnd" jaon:"DashVectorEnd"`
	DashVectorCollection  string             `require:"true" yaml:"DashVectorCollection" jaon:"DashVectorCollection"`
	DashVectorClient      wrapper.HttpClient `yaml:"-" json:"-"`
	DashScopeClient       wrapper.HttpClient `yaml:"-" json:"-"`
}

type OpenaiInfo struct {
	OpenaiServiceName string             `required:"true" yaml:"openaiServiceName" json:"openaiServiceName"`
	OpenaiKey         string             `required:"true" yaml:"openaiKey" json:"openaiKey"`
	OpenaiClient      wrapper.HttpClient `yaml:"-" json:"-"`
}

type KVExtractor struct {
	// @Title zh-CN 从请求 Body 中基于 [GJSON PATH](https://github.com/tidwall/gjson/blob/master/SYNTAX.md) 语法提取字符串
	RequestBody string `required:"false" yaml:"requestBody" json:"requestBody"`
	// @Title zh-CN 从响应 Body 中基于 [GJSON PATH](https://github.com/tidwall/gjson/blob/master/SYNTAX.md) 语法提取字符串
	ResponseBody string `required:"false" yaml:"responseBody" json:"responseBody"`
}

type PluginConfig struct {
	// @Title zh-CN DashVector 阿里云向量搜索引擎
	// @Description zh-CN 调用阿里云的向量搜索引擎
	DashVectorInfo DashVectorInfo `required:"true" yaml:"dashvector" json:"dashvector"`

	OpenaiInfo OpenaiInfo `required:"true" yaml:"openai" json:"openai"`

	SessionID string `yaml:"-" json:"-"`
	// @Title zh-CN Redis 地址信息
	// @Description zh-CN 用于存储缓存结果的 Redis 地址
	RedisInfo RedisInfo `required:"true" yaml:"redis" json:"redis"`
	// @Title zh-CN 缓存 key 的来源
	// @Description zh-CN 往 redis 里存时，使用的 key 的提取方式
	CacheKeyFrom KVExtractor `required:"true" yaml:"cacheKeyFrom" json:"cacheKeyFrom"`
	// @Title zh-CN 缓存 value 的来源
	// @Description zh-CN 往 redis 里存时，使用的 value 的提取方式
	CacheValueFrom KVExtractor `required:"true" yaml:"cacheValueFrom" json:"cacheValueFrom"`
	// @Title zh-CN 流式响应下，缓存 value 的来源
	// @Description zh-CN 往 redis 里存时，使用的 value 的提取方式
	CacheStreamValueFrom KVExtractor `required:"true" yaml:"cacheStreamValueFrom" json:"cacheStreamValueFrom"`
	// @Title zh-CN 返回 HTTP 响应的模版
	// @Description zh-CN 用 %s 标记需要被 cache value 替换的部分
	ReturnResponseTemplate string `required:"true" yaml:"returnResponseTemplate" json:"returnResponseTemplate"`
	// @Title zh-CN 返回流式 HTTP 响应的模版
	// @Description zh-CN 用 %s 标记需要被 cache value 替换的部分
	ReturnTestResponseTemplate string `required:"true" yaml:"returnTestResponseTemplate" json:"returnTestResponseTemplate"`
	ReturnTest                 bool   `required:"false" yaml:"returnTest" json:"returnTest"`

	ReturnStreamResponseTemplate string `required:"true" yaml:"returnStreamResponseTemplate" json:"returnStreamResponseTemplate"`
	// @Title zh-CN 缓存的过期时间
	// @Description zh-CN 单位是秒，默认值为0，即永不过期
	CacheTTL int `required:"false" yaml:"cacheTTL" json:"cacheTTL"`
	// @Title zh-CN Redis缓存Key的前缀
	// @Description zh-CN 默认值是"higress-ai-cache:"
	CacheKeyPrefix string              `required:"false" yaml:"cacheKeyPrefix" json:"cacheKeyPrefix"`
	redisClient    wrapper.RedisClient `yaml:"-" json:"-"`
	LogData        LogData             `yaml:"-" json:"-"`
}

type Embedding struct {
	Embedding []float64 `json:"embedding"`
	TextIndex int       `json:"text_index"`
}

type Input struct {
	Texts []string `json:"texts"`
}

type Params struct {
	TextType string `json:"text_type"`
}

type Response struct {
	RequestID string `json:"request_id"`
	Output    Output `json:"output"`
	Usage     Usage  `json:"usage"`
}

type Output struct {
	Embeddings []Embedding `json:"embeddings"`
}

type Usage struct {
	TotalTokens int `json:"total_tokens"`
}

// EmbeddingRequest 定义请求的数据结构
type EmbeddingRequest struct {
	Model      string `json:"model"`
	Input      Input  `json:"input"`
	Parameters Params `json:"parameters"`
}

// Document 定义每个文档的结构
type Document struct {
	// ID     string            `json:"id"`
	Vector []float64         `json:"vector"`
	Fields map[string]string `json:"fields"`
}

// InsertRequest 定义插入请求的结构
type InsertRequest struct {
	Docs []Document `json:"docs"`
}

type queryList struct {
	key    []string
	chatId []string
	query  []string
	score  []float64
	vector [][]float64
}

type LogData struct {
	SessionID             string    `json:"Session_ID"`
	NewQuery              string    `json:"New_Query"`
	RawQuery              string    `json:"raw_query"`
	Message               []string  `json:"message"`
	ChatID                string    `json:"chat_id"`
	QueryList             []string  `json:"query_list"`
	KeyHistoryString      string    `json:"key_history_string"`
	KeyQueryString        string    `json:"key_query_string"`
	KeyHistoryQueryString string    `json:"key_history_query_string"`
	KeyQueryScore         float64   `json:"key_query_score"`
	KeyHistoryQueryScore  float64   `json:"key_history_query_score"`
	KeyChatID             string    `json:"key_chat_id"`
	KeyChatIDList         []string  `json:"key_chat_id_list"`
	KeyChatScoreList      []float64 `json:"key_chat_score_list"`
	KeyHistoryChatID      string    `json:"key_history_chat_id"`
	CacheType             int       `json:"cache_type"`
	FinalChatQuery        string    `json:"final_chat_query"`
	GetChatID             string    `json:"get_chat_id"`
	RawBody               string    `json:"raw_body"`
	KeyEmbedding          []float64 `json:"key_embedding"`
	KeyHistoryEmbedding   []float64 `json:"key_history_embedding"`
	QueryReasonList       []string  `json:"query_reason_list"`
}

func ConstructTextEmbeddingParameters(c *PluginConfig, log wrapper.Log, texts []string) (string, []byte, [][2]string) {
	url := "/api/v1/services/embeddings/text-embedding/text-embedding"

	data := EmbeddingRequest{
		Model: "text-embedding-v1",
		Input: Input{
			Texts: texts,
		},
		Parameters: Params{
			TextType: "document",
		},
	}

	requestBody, err := json.Marshal(data)
	// requestBody := data
	if err != nil {
		log.Errorf("Failed to marshal request data: %v", err)
		return "", nil, nil
	}

	headers := [][2]string{
		{"Authorization", "Bearer " + c.DashVectorInfo.DashScopeKey},
		{"Content-Type", "application/json"},
	}
	return url, requestBody, headers
}

// QueryResponse 定义查询响应的结构
type QueryResponse struct {
	Code      int      `json:"code"`
	RequestID string   `json:"request_id"`
	Message   string   `json:"message"`
	Output    []Result `json:"output"`
}

// QueryRequest 定义查询请求的结构
type QueryRequest struct {
	Vector        []float64 `json:"vector"`
	TopK          int       `json:"topk"`
	Filter        string    `json:"filter"`
	IncludeVector bool      `json:"include_vector"`
}

// Result 定义查询结果的结构
type Result struct {
	ID     string                 `json:"id"`
	Vector []float64              `json:"vector,omitempty"` // omitempty 使得如果 vector 是空，它将不会被序列化
	Fields map[string]interface{} `json:"fields"`
	Score  float64                `json:"score"`
}

// Define a struct that matches the structure of the JSON response
type ChatCompletionResponse struct {
	Choices []struct {
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Object            string   `json:"object"`
	Usage             struct{} `json:"usage"`
	Created           int64    `json:"created"`
	SystemFingerprint string   `json:"system_fingerprint"`
	Model             string   `json:"model"`
	ID                string   `json:"id"`
}

func ParseTextEmbedding(responseBody []byte) (*Response, error) {
	var resp Response
	err := json.Unmarshal(responseBody, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil

}

// func askLLMForYesorNo(ctx wrapper.HttpContext, config PluginConfig, log wrapper.Log, key string, stream bool) {

// }

func ConstructAskLLMParameters(c PluginConfig, ctx wrapper.HttpContext, log wrapper.Log, content string, useDoc bool) (string, []byte, [][2]string) {
	url := "/compatible-mode/v1/chat/completions"
	messages := []map[string]string{}
	if useDoc {
		messages = []map[string]string{
			{
				"role":    "system",
				"content": "You are a helpful assistant.",
			},
			{
				"role":    "system",
				"content": "fileid://file-fe-6w49htSQwD8vwMqDU6JhkKXV",
			},
			{
				"role":    "user",
				"content": content,
			},
		}
	} else {
		messages = []map[string]string{
			{
				"role":    "system",
				"content": "You are a helpful assistant.",
			},
			{
				"role":    "user",
				"content": content,
			},
		}
	}
	requestData := map[string]interface{}{
		"model":    "qwen-long",
		"messages": messages,
		// "messages": []map[string]string{
		// 	{
		// 		"role":    "system",
		// 		"content": "You are a helpful assistant.",
		// 	},
		// 	{
		// 		"role":    "system",
		// 		"content": "fileid://file-fe-6w49htSQwD8vwMqDU6JhkKXV",
		// 	},
		// 	{
		// 		"role": "user",
		// 		// "content": fmt.Sprintf("已经知道 有问题1: \"%s\", 问题2:\"%s\", 问题1和问题2是同一个问题吗？注意动词之间的细微区别！！！只回答Yes或No！！！其他任何都不许回复,问题1和问题2是同一个问题吗？注意动词之间的细微区别！！！只回答Yes或No！！！其他任何都不许回复,问题1和问题2是同一个问题吗？注意动词之间的细微区别！！！只回答Yes或No！！！其他任何都不许回复", key1, key2),
		// 		"content": content,
		// 	},
		// },
	}

	requestBody, err := json.Marshal(requestData)
	if err != nil {
		log.Errorf("Failed to marshal request data: %v", err)
	}

	header := [][2]string{
		{"Content-Type", "application/json"},
		{"Authorization", "Bearer " + c.DashVectorInfo.DashScopeKey},
	}
	return url, requestBody, header
}

func ConstructAskLLMParametersOpenai(c PluginConfig, ctx wrapper.HttpContext, log wrapper.Log, content string, jsonSchema map[string]interface{}) (string, []byte, [][2]string) {
	url := "/v1/chat/completions"

	// 将 jsonSchema 转换为 JSON 字符串
	jsonSchemaStr, err := json.Marshal(jsonSchema)
	if err != nil {
		// 处理 JSON 序列化错误
		log.Infof("JSON Schema 序列化错误: %v\n", err)
		jsonSchemaStr = []byte("")
	}
	messages := []map[string]string{
		{
			"role": "system",
			"content": `
			假设你是一个初学者，要学习Higress, 你提了一些问题，但是有些问题可能是同一个问题，只是表达方式不同。但是一定要注意问题的主语和动词之间的细微区别！！！比如Higress控制台和Higress的Gateway不是同一个东西，不能视作等价问题！！！
			前提：
			正例:
			`,
		},
		{
			"role":    "user",
			"content": content,
		},
	}
	requestData := map[string]interface{}{
		"model":    "gpt-4o-2024-08-06",
		"messages": messages,
	}
	if len(jsonSchemaStr) > 0 {
		requestData["response_format"] = jsonSchema
	}

	requestBody, err := json.Marshal(requestData)
	if err != nil {
		log.Errorf("Failed to marshal request data: %v", err)
	}

	header := [][2]string{
		{"Content-Type", "application/json"},
		{"Authorization", "Bearer " + c.OpenaiInfo.OpenaiKey},
	}
	return url, requestBody, header
}

// Function to parse the JSON response and return the content
func ParseChatCompletionResponse(jsonData []byte) (string, error) {
	// Parse the JSON data
	var response ChatCompletionResponse
	err := json.Unmarshal(jsonData, &response)
	if err != nil {
		return "", err
	}

	// Check if choices are present and extract the content
	if len(response.Choices) > 0 {
		return response.Choices[0].Message.Content, nil
	}

	return "", fmt.Errorf("no content available in response")
}

func ParseChatCompletionResponseOpenai(responseBody []byte, log wrapper.Log) (int, []string, error) {
	// 将 responseBody 转换为字符串
	jsonData := string(responseBody)

	// 使用 gjson 提取 content 字段
	content := gjson.Get(jsonData, "choices.0.message.content")
	if !content.Exists() {
		return 0, nil, fmt.Errorf("content not found in the response")
	}
	log.Infof("content:%s", content.String())

	// 直接解析 content 字符串为 JSON 对象
	contentJson := content.String()

	// 使用 gjson 提取 finalAnswer 和 reasonList
	_ = gjson.Get(contentJson, "finalAnswer").Int()
	reasonList := gjson.Get(contentJson, "reasonList").Array()

	// 转换 reasonList 为 []string 和提取 isAnswer 为 []bool
	reasons := make([]string, len(reasonList))
	isAnswerList := make([]bool, len(reasonList))

	for i, item := range reasonList {
		// 提取 reason 字符串
		reasons[i] = item.Get("reason").String()

		// 提取 isAnswer 布尔值
		isAnswerList[i] = item.Get("isAnswer").Bool()
		// isAnswerList[i] = true
		// if strings.Contains(reasons[i], "不") || strings.Contains(reasons[i], "没有") {
		// 	isAnswerList[i] = false
		// }
	}

	// 假设 isAnswerList 已经定义并填充了布尔值
	// 定义一个变量来存储结果索引
	var firstTrueIndex int = -1

	// 遍历 isAnswerList 查找第一个不为 false 的索引
	for i, isAnswer := range isAnswerList {
		if isAnswer { // 如果 isAnswer 为 true
			firstTrueIndex = i
			break
		}
	}

	return int(firstTrueIndex), reasons, nil
}

func ConstructEmbeddingQueryParameters(c PluginConfig, vector []float64, filterCommand string, includeVector bool, topK int) (string, []byte, [][2]string, error) {
	url := fmt.Sprintf("/v1/collections/%s/query", c.DashVectorInfo.DashVectorCollection)

	// Initialize requestData with default values
	requestData := QueryRequest{
		TopK:          topK,
		Filter:        filterCommand,
		IncludeVector: includeVector,
	}

	// Only set the Vector field if vector is not nil
	if vector != nil {
		requestData.Vector = vector
	}

	requestBody, err := json.Marshal(requestData)
	if err != nil {
		return "", nil, nil, err
	}

	header := [][2]string{
		{"Content-Type", "application/json"},
		{"dashvector-auth-token", c.DashVectorInfo.DashVectorKey},
	}
	// log.Infof("url:%s, requestBody:%s", url, string(requestBody))
	return url, requestBody, header, nil
}

func ParseQueryResponse(responseBody []byte) (*QueryResponse, error) {
	var resp QueryResponse
	err := json.Unmarshal(responseBody, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func parseQueryOutput(queryResp *QueryResponse) queryList {
	var queryList queryList
	for _, result := range queryResp.Output {
		queryList.key = append(queryList.key, result.Fields["query"].(string))
		queryList.query = append(queryList.query, result.Fields["query"].(string))
		queryList.chatId = append(queryList.chatId, result.Fields["chat_id"].(string))
		queryList.score = append(queryList.score, result.Score)
		if result.Vector != nil {
			queryList.vector = append(queryList.vector, result.Vector)
		}
	}
	return queryList
}

func ConsturctEmbeddingInsertParameters(c *PluginConfig, ctx wrapper.HttpContext, log wrapper.Log, text_embedding []float64, query string, queryType string) (string, []byte, [][2]string, error) {
	// url := fmt.Sprintf("%s/%s/docs", c.DashVectorInfo.DashVectorAuthApiEnd, c.DashVectorInfo.DashVectorCollection)
	url := "/v1/collections/" + c.DashVectorInfo.DashVectorCollection + "/docs"

	key_doc := Document{
		Vector: text_embedding,
		Fields: map[string]string{
			"query":      query,
			"session_id": c.SessionID,
			// "history":    c.GetContext(HistoryStringKey).(string),
			"chat_id":    c.LogData.ChatID,
			"query_type": queryType,
		},
	}

	header := [][2]string{
		{"Content-Type", "application/json"},
		{"dashvector-auth-token", c.DashVectorInfo.DashVectorKey},
	}

	body, err := json.Marshal(InsertRequest{Docs: []Document{key_doc}})
	if err != nil {
		log.Errorf("Failed to marshal request data: %v", err)
	}

	return url, body, header, err
}

func parseConfig(json gjson.Result, c *PluginConfig, log wrapper.Log) error {
	c.ReturnTest = json.Get("returnTest").Bool()
	c.SessionID = json.Get("SessionID").String()
	log.Infof("config:%s", json.Raw)
	c.OpenaiInfo.OpenaiServiceName = json.Get("openai.serviceName").String()
	c.OpenaiInfo.OpenaiKey = json.Get("openai.openaiKey").String()
	c.OpenaiInfo.OpenaiClient = wrapper.NewClusterClient(wrapper.DnsCluster{
		ServiceName: c.OpenaiInfo.OpenaiServiceName,
		Port:        443,
		Domain:      "api.openai.com",
	})
	c.DashVectorInfo.DashScopeKey = json.Get("embeddingProvider.DashScopeKey").String()
	log.Infof("dash scope key:%s", c.DashVectorInfo.DashScopeKey)
	if c.DashVectorInfo.DashScopeKey == "" {
		return errors.New("dash scope key must not by empty")
	}
	log.Infof("dash scope key:%s", c.DashVectorInfo.DashScopeKey)
	c.DashVectorInfo.DashScopeServiceName = json.Get("embeddingProvider.DashScopeServiceName").String()
	c.DashVectorInfo.DashVectorServiceName = json.Get("vectorBaseProvider.DashVectorServiceName").String()
	log.Infof("dash vector service name:%s", c.DashVectorInfo.DashVectorServiceName)
	c.DashVectorInfo.DashVectorKey = json.Get("vectorBaseProvider.DashVectorKey").String()
	log.Infof("dash vector key:%s", c.DashVectorInfo.DashVectorKey)
	if c.DashVectorInfo.DashVectorKey == "" {
		return errors.New("dash vector key must not by empty")
	}
	c.DashVectorInfo.DashVectorAuthApiEnd = json.Get("vectorBaseProvider.DashVectorEnd").String()
	log.Infof("dash vector end:%s", c.DashVectorInfo.DashVectorAuthApiEnd)
	if c.DashVectorInfo.DashVectorAuthApiEnd == "" {
		return errors.New("dash vector end must not by empty")
	}
	c.DashVectorInfo.DashVectorCollection = json.Get("vectorBaseProvider.DashVectorCollection").String()
	log.Infof("dash vector collection:%s", c.DashVectorInfo.DashVectorCollection)

	c.DashVectorInfo.DashVectorClient = wrapper.NewClusterClient(wrapper.DnsCluster{
		ServiceName: c.DashVectorInfo.DashVectorServiceName,
		Port:        443,
		Domain:      c.DashVectorInfo.DashVectorAuthApiEnd,
	})
	c.DashVectorInfo.DashScopeClient = wrapper.NewClusterClient(wrapper.DnsCluster{
		ServiceName: c.DashVectorInfo.DashScopeServiceName,
		Port:        443,
		Domain:      "dashscope.aliyuncs.com",
	})

	c.RedisInfo.ServiceName = json.Get("redis.serviceName").String()
	if c.RedisInfo.ServiceName == "" {
		return errors.New("redis service name must not by empty")
	}
	c.RedisInfo.ServicePort = int(json.Get("redis.servicePort").Int())
	if c.RedisInfo.ServicePort == 0 {
		if strings.HasSuffix(c.RedisInfo.ServiceName, ".static") {
			// use default logic port which is 80 for static service
			c.RedisInfo.ServicePort = 80
		} else {
			c.RedisInfo.ServicePort = 6379
		}
	}
	c.RedisInfo.Username = json.Get("redis.username").String()
	c.RedisInfo.Password = json.Get("redis.password").String()
	c.RedisInfo.Timeout = int(json.Get("redis.timeout").Int())
	if c.RedisInfo.Timeout == 0 {
		c.RedisInfo.Timeout = 1000
	}
	c.CacheKeyFrom.RequestBody = json.Get("cacheKeyFrom.requestBody").String()
	if c.CacheKeyFrom.RequestBody == "" {
		c.CacheKeyFrom.RequestBody = "messages.@reverse.0.content"
	}
	c.CacheValueFrom.ResponseBody = json.Get("cacheValueFrom.responseBody").String()
	if c.CacheValueFrom.ResponseBody == "" {
		c.CacheValueFrom.ResponseBody = "choices.0.message.content"
	}
	c.CacheStreamValueFrom.ResponseBody = json.Get("cacheStreamValueFrom.responseBody").String()
	if c.CacheStreamValueFrom.ResponseBody == "" {
		c.CacheStreamValueFrom.ResponseBody = "choices.0.delta.content"
	}
	c.ReturnResponseTemplate = json.Get("returnResponseTemplate").String()
	if c.ReturnResponseTemplate == "" {
		c.ReturnResponseTemplate = `{"id":"from-cache","choices":[{"index":0,"message":{"role":"assistant","content":"%s"},"finish_reason":"stop"}],"model":"gpt-4o","object":"chat.completion","usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}}`
	}
	c.ReturnStreamResponseTemplate = json.Get("returnStreamResponseTemplate").String()
	if c.ReturnStreamResponseTemplate == "" {
		c.ReturnStreamResponseTemplate = `data:{"id":"from-cache","choices":[{"index":0,"delta":{"role":"assistant","content":"%s"},"finish_reason":"stop"}],"model":"gpt-4o","object":"chat.completion","usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}}` + "\n\ndata:[DONE]\n\n"
	}
	c.ReturnTestResponseTemplate = json.Get("returnTestResponseTemplate").String()
	if c.ReturnTestResponseTemplate == "" {
		c.ReturnTestResponseTemplate = `{"id":"random-generate","choices":[{"index":0,"message":{"role":"assistant","content":"%s"},"finish_reason":"stop"}],"model":"gpt-4o","object":"chat.completion","usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}}`
	}
	c.CacheKeyPrefix = json.Get("cacheKeyPrefix").String()
	if c.CacheKeyPrefix == "" {
		c.CacheKeyPrefix = DefaultCacheKeyPrefix
	}
	c.redisClient = wrapper.NewRedisClusterClient(wrapper.FQDNCluster{
		FQDN: c.RedisInfo.ServiceName,
		Port: int64(c.RedisInfo.ServicePort),
	})
	return c.redisClient.Init(c.RedisInfo.Username, c.RedisInfo.Password, int64(c.RedisInfo.Timeout))
}

func onHttpRequestHeaders(ctx wrapper.HttpContext, config PluginConfig, log wrapper.Log) types.Action {
	contentType, _ := proxywasm.GetHttpRequestHeader("content-type")
	// The request does not have a body.
	if contentType == "" {
		return types.ActionContinue
	}
	if !strings.Contains(contentType, "application/json") {
		log.Warnf("content is not json, can't process:%s", contentType)
		ctx.DontReadRequestBody()
		return types.ActionContinue
	}
	proxywasm.RemoveHttpRequestHeader("Accept-Encoding")
	// The request has a body and requires delaying the header transmission until a cache miss occurs,
	// at which point the header should be sent.
	return types.HeaderStopIteration
}

func TrimQuote(source string) string {
	return strings.Trim(source, `"`)
}

// ===================== 以下是主要逻辑 =====================
// 主handler函数，根据key从redis中获取value ，如果不命中，则首先调用文本向量化接口向量化query，然后调用向量搜索接口搜索最相似的出现过的key，最后再次调用redis获取结果
// 可以把所有handler单独提取为文件，这里为了方便读者复制就和主逻辑放在一个文件中了
//
// 1. query 进来和 redis 中存的 key 匹配 (redisSearchHandler) ，若完全一致则直接返回 (handleCacheHit)
// 2. 否则请求 text_embdding 接口将 query 转换为 query_embedding (fetchAndProcessEmbeddings)
// 3. 用 query_embedding 和向量数据库中的向量做 ANN search，返回最接近的 key ，并用阈值过滤 (performQueryAndRespond)
// 4. 若返回结果为空或大于阈值，舍去，本轮 cache 未命中, 最后将 query_embedding 存入向量数据库 (uploadQueryEmbedding)
// 5. 若小于阈值，则再次调用 redis对 most similar key 做匹配。 (redisSearchHandler)
// 7. 在 response 阶段请求 redis 新增key/LLM返回结果

func redisSearchHandler(key string, ctx wrapper.HttpContext, config PluginConfig, log wrapper.Log, stream bool, ifUseEmbedding bool) {
	redisKey := config.SessionID + key
	config.redisClient.Get(redisKey, func(response resp.Value) {
		if err := response.Error(); err == nil && !response.IsNull() {
			handleCacheHit(key, response, stream, ctx, config, log)
		} else {
			// log.Warnf("cache miss, key:%s", key)
			if ifUseEmbedding {
				handleCacheMiss(key, err, response, ctx, config, log, key, stream)
			} else {
				log.Warnf("the key not in cache, key:%s", key)
				config.LogData.CacheType = -99
				config.LogData.GetChatID = ""
				logAndResume(ctx, config, log)
				return
			}
		}
	})
}

// 简单处理缓存命中的情况, 从redis中获取到value后，直接返回
func handleCacheHit(key string, response resp.Value, stream bool, ctx wrapper.HttpContext, config PluginConfig, log wrapper.Log) {
	log.Warnf("cache hit, key:%s", key)
	ctx.SetContext(CacheKeyContextKey, nil)
	logAndReturn(config, log, stream, response.String())
}

// 处理缓存未命中的情况，调用fetchAndProcessEmbeddings函数向量化query
func handleCacheMiss(key string, err error, response resp.Value, ctx wrapper.HttpContext, config PluginConfig, log wrapper.Log, queryString string, stream bool) {
	if err != nil {
		log.Warnf("redis get key:%s failed, err:%v", key, err)
	}
	if response.IsNull() {
		log.Warnf("cache miss, key:%s", key)
	}
	// 先去dashvector中查询是否有已经向量化的query
	// fetchAndProcessEmbeddings(key, ctx, config, log, queryString, stream)
	fetchQueryEmbedding(ctx, config, log, key, stream)
}

// func logAndResume(config PluginConfig, log wrapper.Log) {
// 	json_logData, _ := json.Marshal(config.LogData)
// 	log.Infof("%s", json_logData)
// 	proxywasm.ResumeHttpRequest()
// }

func fetchQueryEmbedding(ctx wrapper.HttpContext, config PluginConfig, log wrapper.Log, key string, stream bool) {
	filterCommand := fmt.Sprintf("query = \"%s\" and query_type = \"cache\"", key)
	embedding_url, embedding_requestBody, embedding_headers, err := ConstructEmbeddingQueryParameters(config, nil, filterCommand, true, 1)

	if err != nil {
		log.Errorf("Failed to construct embedding query parameters: %v", err)
		logAndResume(ctx, config, log)
		return
	}

	config.DashVectorInfo.DashVectorClient.Post(
		embedding_url,
		embedding_headers,
		embedding_requestBody,
		func(statusCode int, responseHeaders http.Header, responseBody []byte) {
			if statusCode != 200 {
				log.Errorf("Failed to fetch query embedding, statusCode: %d, responseBody: %s", statusCode, string(responseBody))
				logAndResume(ctx, config, log)
				return
			}
			queryResp, err := ParseQueryResponse(responseBody)
			if err != nil {
				log.Errorf("Failed to parse query response: %v", err)
				logAndResume(ctx, config, log)
				return
			}
			queryList := parseQueryOutput(queryResp)
			// 未命中，调用文本向量化接口向量化query
			if len(queryList.key) == 0 {
				log.Warnf("No query found in dashvector, key:%s", key)
				fetchAndProcessEmbeddings(key, ctx, config, log, key, stream)
				return
			} else {
				log.Infof("Successfully fetched cached embedding for key: %s", key)
				ctx.SetContext(QueryEmbeddingKey, queryList.vector[0])
				processFetchedEmbeddings(key, queryList.vector[0], ctx, config, log, stream)
			}
		},
		100000)

}

func uploadQueryEmbeddingCache(ctx wrapper.HttpContext, config PluginConfig, log wrapper.Log, key string, key_embedding []float64, stream bool) {
	// key_embedding := ctx.GetContext(QueryEmbeddingKey).([]float64)
	// key_embedding := ctx.GetContext(QueryEmbeddingKey).([]float64)
	vector_url, vector_body, vector_headers, _ := ConsturctEmbeddingInsertParameters(&config, ctx, log, key_embedding, key, "cache")

	config.DashVectorInfo.DashVectorClient.Post(
		vector_url,
		vector_headers,
		vector_body,
		func(statusCode int, responseHeaders http.Header, responseBody []byte) {
			if statusCode != 200 {
				log.Errorf("Failed to upload query embedding, statusCode: %d, responseBody: %s", statusCode, string(responseBody))
				logAndResume(ctx, config, log)
				return
			}
			log.Infof("Successfully uploaded cache embedding for key: %s", key)
			// redisSearchHandler(key, ctx, config, log, stream, false)
			processFetchedEmbeddings(key, key_embedding, ctx, config, log, stream)
		},
		100000,
	)
}

func logAndResume(ctx wrapper.HttpContext, config PluginConfig, log wrapper.Log) {
	json_logData, _ := json.Marshal(config.LogData)
	log.Infof("%s", json_logData)

	// newUUID := uuid.New()
	// config.redisClient.Set(config.SessionID+config.LogData.NewQuery, "Temp_save:"+newUUID.String(), nil)
	// // log.Infof("%v", config.ReturnTest)
	// if config.ReturnTest {
	// 	key := config.LogData.NewQuery
	// 	// log.Infof("I am processing cache to redis, key:%s, value:%s", key, "你好吗？这里是测试")
	// 	config.redisClient.Set(config.SessionID+key, "你好吗？这里是测试", nil)
	// 	proxywasm.SendHttpResponse(200, [][2]string{{"content-type", "application/json; charset=utf-8"}}, []byte(fmt.Sprintf(config.ReturnTestResponseTemplate, "你好吗？这里是测试")), -1)
	// } else {
	// 	proxywasm.ResumeHttpRequest()
	// }
	newUUID := uuid.New()
	config.redisClient.Set(config.SessionID+config.LogData.NewQuery, "Temp_save:"+newUUID.String(),
		func(response resp.Value) {
			if response.Error() != nil {
				log.Errorf("Failed to set key:%s, err:%v", config.LogData.NewQuery, response.Error())
			}
			log.Infof("Successfully set temp value for key:%s", config.LogData.NewQuery)
			// newUUID := uuid.New()
			// config.redisClient.Set(config.SessionID+config.LogData.NewQuery, "Temp_save:"+newUUID.String(), nil)
			// log.Infof("%v", config.ReturnTest)
			if config.ReturnTest {
				key := config.LogData.NewQuery
				// log.Infof("I am processing cache to redis, key:%s, value:%s", key, "你好吗？这里是测试")
				config.redisClient.Set(config.SessionID+key, "你好吗？这里是测试", nil)
				proxywasm.SendHttpResponse(200, [][2]string{{"content-type", "application/json; charset=utf-8"}}, []byte(fmt.Sprintf(config.ReturnTestResponseTemplate, "你好吗？这里是测试")), -1)
			} else {
				proxywasm.ResumeHttpRequest()
			}
		})
}

func logAndReturn(config PluginConfig, log wrapper.Log, stream bool, content string) {
	json_logData, _ := json.Marshal(config.LogData)
	log.Infof("%s", json_logData)
	if !stream {
		proxywasm.SendHttpResponse(200, [][2]string{{"content-type", "application/json; charset=utf-8"}}, []byte(fmt.Sprintf(config.ReturnResponseTemplate, content)), -1)
	} else {
		proxywasm.SendHttpResponse(200, [][2]string{{"content-type", "text/event-stream; charset=utf-8"}}, []byte(fmt.Sprintf(config.ReturnStreamResponseTemplate, content)), -1)
	}
}

// 调用文本向量化接口向量化query, 向量化成功后调用processFetchedEmbeddings函数处理向量化结果
func fetchAndProcessEmbeddings(key string, ctx wrapper.HttpContext, config PluginConfig, log wrapper.Log, queryString string, stream bool) {
	Emb_url, Emb_requestBody, Emb_headers := ConstructTextEmbeddingParameters(&config, log, []string{queryString})
	config.DashVectorInfo.DashScopeClient.Post(
		Emb_url,
		Emb_headers,
		Emb_requestBody,
		func(statusCode int, responseHeaders http.Header, responseBody []byte) {
			// log.Infof("statusCode:%d, responseBody:%s", statusCode, string(responseBody))
			log.Infof("Successfully fetched embeddings for key: %s", key)
			if statusCode != 200 {
				log.Errorf("Failed to fetch embeddings, statusCode: %d, responseBody: %s", statusCode, string(responseBody))
				ctx.SetContext(QueryEmbeddingKey, nil)
				logAndResume(ctx, config, log)
			} else {
				text_embedding_raw, _ := ParseTextEmbedding(responseBody)
				text_embedding := text_embedding_raw.Output.Embeddings[0].Embedding
				// processFetchedEmbeddings(key, text_embedding, ctx, config, log, stream)
				uploadQueryEmbeddingCache(ctx, config, log, key, text_embedding, stream)
			}
		},
		100000)
}

// 先将向量化的结果存入上下文ctx变量，其次发起向量搜索请求
func processFetchedEmbeddings(key string, text_embedding []float64, ctx wrapper.HttpContext, config PluginConfig, log wrapper.Log, stream bool) {
	log.Infof("Start to process fetched embeddings for key: %s", key)
	// text_embedding := text_embedding_raw.Output.Embeddings[0].Embedding
	// ctx.SetContext(CacheKeyContextKey, text_embedding)
	ctx.SetContext(QueryEmbeddingKey, text_embedding)
	ctx.SetContext(CacheKeyContextKey, key)
	performQueryAndRespond(key, text_embedding, ctx, config, log, stream)
}

// Function to extract the reconstructed question
func ExtractReconstructedQuestion(queryAns string) string {
	// Use regex to find the text inside square brackets []
	re := regexp.MustCompile(`\[(.*?)\]`)
	matches := re.FindStringSubmatch(queryAns)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

func askLLMForRefineQuery(ctx wrapper.HttpContext, config PluginConfig, log wrapper.Log, key string, messages []string, stream bool) {
	// 初始化倒数第三个和倒数第二个元素为空字符串
	thirdLastMessage := ""
	secondLastMessage := ""

	// 检查切片的长度是否足够
	if len(messages) >= 2 {
		thirdLastMessage = messages[len(messages)-2]
	}
	if len(messages) >= 3 {
		secondLastMessage = messages[len(messages)-3]
	}

	// 拼接倒数第三个和倒数第二个元素
	combinedMessage := strings.Join([]string{thirdLastMessage, secondLastMessage}, ",")

	// 去掉可能在开头或结尾的逗号
	combinedMessage = strings.Trim(combinedMessage, ",")

	content := fmt.Sprintf(
		"之前提问过: \"%s\"， 当前问题是\"%s\", 请联系历史问题帮我简洁的重构当前问题，问题本身需要加上“用最简洁的一句话”，最后用[]包裹重构后的问题!!!用[]包裹重构后的问题!!!用[]包裹重构后的问题!!!",
		combinedMessage, // All messages except the last one
		key,
	)
	log.Infof("content:%s", content)
	url, requestBody, header := ConstructAskLLMParameters(config, ctx, log, content, false)
	config.DashVectorInfo.DashScopeClient.Post(
		url,
		header,
		requestBody,
		func(statusCode int, responseHeaders http.Header, responseBody []byte) {
			queryAns, err := ParseChatCompletionResponse(responseBody)
			log.Infof("For the short query, statusCode:%d, queryAns:%s", statusCode, queryAns)
			if err != nil {
				log.Errorf("Failed to parse response: %v", err)
				logAndResume(ctx, config, log)
				return
			}
			// Extract the new question from queryAns
			newQuestion := ExtractReconstructedQuestion(queryAns)
			if newQuestion == "" {
				key = queryAns
			} else {
				key = newQuestion
			}
			ctx.SetContext(CacheKeyContextKey, key)
			config.LogData.NewQuery = key
			redisSearchHandler(key, ctx, config, log, stream, true)
		},
		100000,
	)
}

func askLLMForRank(ctx wrapper.HttpContext, config PluginConfig, log wrapper.Log, key1 string, key2List queryList, index int, textEmbedding []float64) {
	// log.Infof("len(key2List):%d, index:%d", len(key2List.key), index)
	if index == len(key2List.key) {
		log.Warnf("no key match, exit, cacheType:-3")
		config.LogData.CacheType = -3
		uploadQueryEmbedding(ctx, config, log, key1, textEmbedding)
		// logAndResume(ctx, config, log)
		return
	}

	key2 := key2List.key[index]
	config.LogData.KeyQueryString = key2
	config.LogData.KeyChatID = key2List.chatId[index]
	config.LogData.KeyQueryScore = key2List.score[index]

	content := fmt.Sprintf(
		`
			假设你是一个初学者，要学习Higress, 你提了一些问题，但是有些问题可能是同一个问题，只是表达方式不同。但是一定要注意问题的主语和动词之间的细微区别！！！比如Higress控制台和Higress的Gateway不是同一个东西，不能视作等价问题！！！
			前提：
			正例:
			"打开配置"和"启用配置"是一个意思。
			"激活 Higress 控制台的监控聚合？" 和 “为 Higress Console 启用监控聚合？” 是一个意思！！！
			反例:
			"JWT Token "只属于一种token，不等同于"token"。

	问题1: “%s”,
	问题2: “%s”,

	根据以上前提，问题1和问题2想要得到的回答的答案是相同的吗？只说Yes或No，其他任何都不要显示！！！
	根据以上前提，问题1和问题2想要得到的回答的答案是相同的吗？只说Yes或No，其他任何都不要显示！！！
	根据以上前提，问题1和问题2想要得到的回答的答案是相同的吗？只说Yes或No，其他任何都不要显示！！！

	根据以上前提,问题1和问题2想要得到的回答的答案是相同的吗？只说Yes或No，其他任何都不要显示！！！`,
		key1, key2)
	url, requestBody, header := ConstructAskLLMParameters(config, ctx, log, content, false)
	config.DashVectorInfo.DashScopeClient.Post(
		url,
		header,
		requestBody,
		func(statusCode int, responseHeaders http.Header, responseBody []byte) {
			queryAns, err := ParseChatCompletionResponse(responseBody)
			log.Infof("statusCode:%d, queryAns:%s", statusCode, queryAns)
			if err != nil {
				log.Errorf("Failed to parse response: %v", err)
				logAndResume(ctx, config, log)
				return
			}
			keyMatch := true
			// if queryAns == "Yes" || queryAns == "yes" {
			// 	keyMatch = true
			// }
			if strings.Contains(strings.ToLower(queryAns), "no") {
				keyMatch = false
			}
			if keyMatch && key2List.score[index] < 10000 {
				config.LogData.CacheType = 4
				config.LogData.FinalChatQuery = key2
				config.LogData.GetChatID = config.LogData.KeyChatID
				ctx.SetContext(CacheKeyContextKey, nil)
				redisSearchHandler(key2, ctx, config, log, false, false)
				// logAndReturn(config, log, false, key2)
			} else {
				askLLMForRank(ctx, config, log, key1, key2List, index+1, textEmbedding)
			}
		},
		100000,
	)
}

func askLLMForRankOpenai(ctx wrapper.HttpContext, config PluginConfig, log wrapper.Log, key1 string, key2List queryList, index int, textEmbedding []float64) {
	// log.Infof("len(key2List):%d, index:%d", len(key2List.key), index)
	if index == len(key2List.key) {
		log.Warnf("no key match, exit, cacheType:-3")
		config.LogData.CacheType = -3
		uploadQueryEmbedding(ctx, config, log, key1, textEmbedding)
		// logAndResume(ctx, config, log)
		return
	}

	key2 := key2List.key[index]
	config.LogData.KeyQueryString = key2
	config.LogData.KeyChatID = key2List.chatId[index]
	config.LogData.KeyQueryScore = key2List.score[index]

	// 创建一个字符串来存储索引和值
	var sb strings.Builder
	for i, val := range key2List.query {
		sb.WriteString(fmt.Sprintf("index %d: %s, ", i, val))
	}
	// 去掉最后一个多余的逗号和空格
	queryList := strings.TrimSuffix(sb.String(), ", ")

	content := fmt.Sprintf(
		`给定一个查询问题 %s 和一组候选问题列表 %s，我们想知道查询问题的回答是否能在候选问题的回答中找到完全相同的表述（请谨慎选择，必须是两个回答会字面上一模一样的才能用于选择!）。请返回满足条件的候选问题的序号（从0开始计数）。如果有多个候选问题和查询问题相同，返回序号较小的那个。如果所有问题都不同，请返回 -1。`, key1, queryList)

	jsonSchema := map[string]interface{}{
		"type": "json_schema",
		"json_schema": map[string]interface{}{
			"name":   "AskLLMForRank",
			"strict": true,
			"schema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"reasonList": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"reason": map[string]interface{}{
									"type":        "string",
									"description": "选或者不选的理由，请分析问题中面向的主语和动词，仔细判断他们的区别，确定是否是完全一致的。注意是完全一致！！！",
								},
								"isAnswer": map[string]interface{}{
									"type":        "boolean",
									"description": "判断该理由是否为最终答案。",
								},
							},
							"required":             []string{"reason", "isAnswer"},
							"additionalProperties": false, // 添加这一行
						},
						"description": "选或者不选的理由列表，每个元素是一个包含理由（字符串）和答案判断（布尔值）的对象。",
					},
					"finalAnswer": map[string]interface{}{
						"type":        "integer",
						"description": "最终的答案，必须为 -1 到 reasonList 的最大索引之间的整数。",
					},
				},
				"required":             []string{"reasonList", "finalAnswer"},
				"additionalProperties": false,
			},
		},
	}

	url, requestBody, header := ConstructAskLLMParametersOpenai(config, ctx, log, content, jsonSchema)
	config.OpenaiInfo.OpenaiClient.Post(
		url,
		header,
		requestBody,
		func(statusCode int, responseHeaders http.Header, responseBody []byte) {
			// log.Infof("statusCode:%d, responseBody:%s", statusCode, string(responseBody))
			queryAns, reasonList, err := ParseChatCompletionResponseOpenai(responseBody, log)
			log.Infof("statusCode:%d, queryAns:%d, reasonList:%v", statusCode, queryAns, reasonList)
			config.LogData.QueryReasonList = reasonList
			if err != nil {
				log.Errorf("Failed to parse response: %v", err)
				logAndResume(ctx, config, log)
				return
			}
			keyMatch := false
			index := 0
			if queryAns == -1 || queryAns >= len(key2List.query) {
				keyMatch = false
			} else {
				keyMatch = true
				index = queryAns
			}
			if keyMatch && key2List.score[index] < 10000 {
				config.LogData.CacheType = 4
				config.LogData.FinalChatQuery = key2
				config.LogData.GetChatID = config.LogData.KeyChatID
				ctx.SetContext(CacheKeyContextKey, nil)
				redisSearchHandler(key2, ctx, config, log, false, false)
				// logAndReturn(config, log, false, key2)
			} else {
				log.Warnf("no key match, exit, cacheType:-3")
				config.LogData.CacheType = -3
				uploadQueryEmbedding(ctx, config, log, key1, textEmbedding)
			}
		},
		100000,
	)
}

// 调用向量搜索接口搜索最相似的key，搜索成功后调用redisSearchHandler函数获取最相似的key的结果
func performQueryAndRespond(key string, text_embedding []float64, ctx wrapper.HttpContext, config PluginConfig, log wrapper.Log, stream bool) {
	filterCommand := fmt.Sprintf("session_id = \"%s\" and query_type = \"%s\"", config.SessionID, "key")
	vector_url, vector_request, vector_headers, err := ConstructEmbeddingQueryParameters(config, text_embedding, filterCommand, false, 4)
	// vector_url, vector_request, vector_headers, err := ConstructEmbeddingQueryParameters(config, text_embedding, filterCommand, false, 5)
	// log.Infof("vector_url:%s, vector_request:%s", vector_url, string(vector_request))
	if err != nil {
		log.Errorf("Failed to perform query, err: %v", err)
		proxywasm.ResumeHttpRequest()
		return
	}
	config.DashVectorInfo.DashVectorClient.Post(
		vector_url,
		vector_headers,
		vector_request,
		func(statusCode int, responseHeaders http.Header, responseBody []byte) {
			log.Infof("statusCode:%d, responseBody:%s", statusCode, string(responseBody))
			query_resp, err_query := ParseQueryResponse(responseBody)
			if err_query != nil {
				log.Errorf("Failed to parse response: %v", err)
				logAndResume(ctx, config, log)
				return
			}
			if len(query_resp.Output) < 1 {
				config.LogData.CacheType = -1
				log.Warnf("query response is empty")
				uploadQueryEmbedding(ctx, config, log, key, text_embedding)
				return
			}
			log.Infof("We have %d results", len(query_resp.Output))
			// 现在这一步会获得k个结果, 再通过LLM判断是否是同一个问题
			queryList := parseQueryOutput(query_resp)
			config.LogData.KeyChatIDList = queryList.chatId
			config.LogData.KeyChatScoreList = queryList.score
			askLLMForRank(ctx, config, log, key, queryList, 0, text_embedding)
			// most_similar_key := query_resp.Output[0].Fields["query"].(string)
			// log.Infof("most similar key:%s", most_similar_key)
			// most_similar_score := query_resp.Output[0].Score
			// config.LogData.KeyQueryString = most_similar_key
			// config.LogData.KeyQueryScore = most_similar_score
			// config.LogData.KeyChatID = query_resp.Output[0].Fields["chat_id"].(string)
			// if most_similar_score < 5000 {
			// 	config.LogData.CacheType = 0
			// 	// config.LogData.GetChatID = config.LogData.KeyChatID
			// 	// config.LogData.FinalChatQuery = most_similar_key
			// 	askLLMForRank(ctx, config, log, key, most_similar_key)
			// 	// ctx.SetContext(CacheKeyContextKey, nil)
			// 	// redisSearchHandler(most_similar_key, ctx, config, log, stream, false)
			// } else {
			// 	config.LogData.CacheType = -2
			// 	log.Infof("the most similar key's score is too high, key:%s, score:%f", most_similar_key, most_similar_score)
			// 	uploadQueryEmbedding(ctx, config, log, key, text_embedding)
			// 	logAndResume(ctx, config, log)
			// 	return
			// }
		},
		100000)
}

// 未命中cache，则将新的query embedding和对应的key存入向量数据库
func uploadQueryEmbedding(ctx wrapper.HttpContext, config PluginConfig, log wrapper.Log, key string, text_embedding []float64) error {
	vector_url, vector_body, vector_headers, err := ConsturctEmbeddingInsertParameters(&config, ctx, log, text_embedding, key, "key")
	// log.Infof("vector_url:%s, vector_body:%s", vector_url, string(vector_body))
	if err != nil {
		log.Errorf("Failed to construct embedding insert parameters: %v", err)
		logAndResume(ctx, config, log)
		return nil
	}
	err = config.DashVectorInfo.DashVectorClient.Post(
		vector_url,
		vector_headers,
		vector_body,
		func(statusCode int, responseHeaders http.Header, responseBody []byte) {
			if statusCode != 200 {
				log.Errorf("Failed to upload query embedding: %s", responseBody)
			} else {
				log.Infof("Successfully uploaded query embedding for key: %s", key)
			}
			logAndResume(ctx, config, log)
		},
		100000,
	)
	if err != nil {
		log.Errorf("Failed to upload query embedding: %v", err)
		logAndResume(ctx, config, log)
		return nil
	}
	// logAndResume(ctx, config, log)
	return nil
}

// ===================== 以上是主要逻辑 =====================

func generateKeyHistory(messages []string, key string) string {
	var builder strings.Builder
	builder.WriteString("history:")

	// 添加除最后一个之外的所有消息
	if len(messages) > 1 {
		builder.WriteString(strings.Join(messages[:len(messages)-1], ""))
	}

	builder.WriteString("user question:")

	// 将key字符串重复多次，次数与messages的长度相同
	for i := 0; i < len(messages); i++ {
		builder.WriteString(key)
	}

	return builder.String()
}

func ParseQueryAndHistory(bodyJson gjson.Result, config PluginConfig, log wrapper.Log, byteLen int) (string, []string, string) {
	// 获取所有的用户消息历史
	userMessages := bodyJson.Get(`messages.#(role=="user")#.content`)
	var messages []string
	for _, result := range userMessages.Array() {
		append_string := strings.Trim(strings.ReplaceAll(result.String(), "?", "？"), " ")
		messages = append(messages, append_string)
	}
	key, err := strconv.Unquote(strings.ReplaceAll(bodyJson.Get(config.CacheKeyFrom.RequestBody).Raw, `\\`, `\`))
	key = strings.ReplaceAll(key, `?`, `？`)
	if err != nil {
		log.Warnf("Failed to unquote key: %v", err)
		key = bodyJson.Get(config.CacheKeyFrom.RequestBody).String()
	}
	len_messages := len(messages)

	// if len(key) < byteLen {
	// 	// 拼接上最后一个大于byte_len的消息
	// 	repeat_key := key // 初始化 repeat_key 为 key
	// 	for len(repeat_key) < byteLen {
	// 		repeat_key += key // 拼接 key 到 repeat_key
	// 	}
	// 	key = "Please answer that:" + repeat_key
	// 	for i := len(messages) - 2; i >= 0; i-- {
	// 		key = messages[i] + key
	// 		if len(messages[i]) >= byteLen {
	// 			break
	// 		}
	// 	}
	// 	key = "Given the history:" + key
	// }
	log.Infof("Received Query: %s with message len: %d", key, len_messages)

	key_histoy := generateKeyHistory(messages, key)
	return key, messages, key_histoy
}

// 主要修改函数，将原有的redis缓存策略逻辑替换为新的基于向量的缓存策略逻辑。
func onHttpRequestBody(ctx wrapper.HttpContext, config PluginConfig, body []byte, log wrapper.Log) types.Action {
	bodyJson := gjson.ParseBytes(body)

	ctx.SetContext("SessionID", config.SessionID)
	chat_id := uuid.New().String()
	ctx.SetContext("ChatID", chat_id)

	ctx.SetContext("threshold", 0.1)
	ctx.SetContext("keyScoreThreshold", 0.18)
	ctx.SetContext("keyHisScoreThreshold", 0.17)
	ctx.SetContext("byteLen", 31)

	key, messages, key_history := ParseQueryAndHistory(bodyJson, config, log, ctx.GetContext("byteLen").(int))

	config.LogData = LogData{
		SessionID:             config.SessionID,
		NewQuery:              key,
		RawQuery:              bodyJson.Get(config.CacheKeyFrom.RequestBody).String(),
		Message:               messages,
		ChatID:                chat_id,
		KeyHistoryString:      key_history,
		QueryList:             []string{},
		KeyQueryString:        "",
		KeyHistoryQueryString: "",
		KeyQueryScore:         0.999,
		KeyHistoryQueryScore:  0.999,
		KeyChatID:             "",
		KeyHistoryChatID:      "",
		CacheType:             -1,
		FinalChatQuery:        "",
		GetChatID:             "",
		RawBody:               "",
		QueryReasonList:       []string{},
	}
	// TODO: It may be necessary to support stream mode determination for different LLM providers.
	stream := false
	if bodyJson.Get("stream").Bool() {
		stream = true
		ctx.SetContext(StreamContextKey, struct{}{})
	} else if ctx.GetContext(StreamContextKey) != nil {
		stream = true
	}
	// key := TrimQuote(bodyJson.Get(config.CacheKeyFrom.RequestBody).Raw)

	if len(key) < ctx.GetContext("byteLen").(int) {
		askLLMForRefineQuery(ctx, config, log, key, messages, stream)
	} else {
		redisSearchHandler(key, ctx, config, log, stream, true)
	}

	return types.ActionPause
}

func processSSEMessage(ctx wrapper.HttpContext, config PluginConfig, sseMessage string, log wrapper.Log) string {
	subMessages := strings.Split(sseMessage, "\n")
	var message string
	for _, msg := range subMessages {
		if strings.HasPrefix(msg, "data:") {
			message = msg
			break
		}
	}
	if len(message) < 6 {
		log.Warnf("invalid message:%s", message)
		return ""
	}
	// skip the prefix "data:"
	bodyJson := message[5:]
	if gjson.Get(bodyJson, config.CacheStreamValueFrom.ResponseBody).Exists() {
		tempContentI := ctx.GetContext(CacheContentContextKey)
		if tempContentI == nil {
			content := TrimQuote(gjson.Get(bodyJson, config.CacheStreamValueFrom.ResponseBody).Raw)
			ctx.SetContext(CacheContentContextKey, content)
			return content
		}
		append := TrimQuote(gjson.Get(bodyJson, config.CacheStreamValueFrom.ResponseBody).Raw)
		content := tempContentI.(string) + append
		ctx.SetContext(CacheContentContextKey, content)
		return content
	} else if gjson.Get(bodyJson, "choices.0.delta.content.tool_calls").Exists() {
		// TODO: compatible with other providers
		ctx.SetContext(ToolCallsContextKey, struct{}{})
		return ""
	}
	log.Warnf("unknown message:%s", bodyJson)
	return ""
}

func onHttpResponseHeaders(ctx wrapper.HttpContext, config PluginConfig, log wrapper.Log) types.Action {
	contentType, _ := proxywasm.GetHttpResponseHeader("content-type")
	if strings.Contains(contentType, "text/event-stream") {
		ctx.SetContext(StreamContextKey, struct{}{})
	}
	return types.ActionContinue
}

func onHttpResponseBody(ctx wrapper.HttpContext, config PluginConfig, chunk []byte, isLastChunk bool, log wrapper.Log) []byte {
	// log.Infof("I am here")
	if ctx.GetContext(ToolCallsContextKey) != nil {
		// we should not cache tool call result
		return chunk
	}
	keyI := ctx.GetContext(CacheKeyContextKey)
	// log.Infof("I am here 2: %v", keyI)
	if keyI == nil {
		return chunk
	}
	if !isLastChunk {
		stream := ctx.GetContext(StreamContextKey)
		if stream == nil {
			tempContentI := ctx.GetContext(CacheContentContextKey)
			if tempContentI == nil {
				ctx.SetContext(CacheContentContextKey, chunk)
				return chunk
			}
			tempContent := tempContentI.([]byte)
			tempContent = append(tempContent, chunk...)
			ctx.SetContext(CacheContentContextKey, tempContent)
		} else {
			var partialMessage []byte
			partialMessageI := ctx.GetContext(PartialMessageContextKey)
			if partialMessageI != nil {
				partialMessage = append(partialMessageI.([]byte), chunk...)
			} else {
				partialMessage = chunk
			}
			messages := strings.Split(string(partialMessage), "\n\n")
			for i, msg := range messages {
				if i < len(messages)-1 {
					// process complete message
					processSSEMessage(ctx, config, msg, log)
				}
			}
			if !strings.HasSuffix(string(partialMessage), "\n\n") {
				ctx.SetContext(PartialMessageContextKey, []byte(messages[len(messages)-1]))
			} else {
				ctx.SetContext(PartialMessageContextKey, nil)
			}
		}
		return chunk
	}
	// last chunk
	key := keyI.(string)
	stream := ctx.GetContext(StreamContextKey)
	var value string
	if stream == nil {
		var body []byte
		tempContentI := ctx.GetContext(CacheContentContextKey)
		if tempContentI != nil {
			body = append(tempContentI.([]byte), chunk...)
		} else {
			body = chunk
		}
		bodyJson := gjson.ParseBytes(body)

		value = TrimQuote(bodyJson.Get(config.CacheValueFrom.ResponseBody).Raw)
		if value == "" {
			log.Warnf("parse value from response body failded, body:%s", body)
			return chunk
		}
	} else {
		if len(chunk) > 0 {
			var lastMessage []byte
			partialMessageI := ctx.GetContext(PartialMessageContextKey)
			if partialMessageI != nil {
				lastMessage = append(partialMessageI.([]byte), chunk...)
			} else {
				lastMessage = chunk
			}
			if !strings.HasSuffix(string(lastMessage), "\n\n") {
				log.Warnf("invalid lastMessage:%s", lastMessage)
				return chunk
			}
			// remove the last \n\n
			lastMessage = lastMessage[:len(lastMessage)-2]
			value = processSSEMessage(ctx, config, string(lastMessage), log)
		} else {
			tempContentI := ctx.GetContext(CacheContentContextKey)
			if tempContentI == nil {
				return chunk
			}
			value = tempContentI.(string)
		}
	}
	// log.Infof("I am processing cache to redis, key:%s, value:%s", key, value)
	config.redisClient.Set(config.SessionID+key, value, nil)
	if config.CacheTTL != 0 {
		config.redisClient.Expire(config.SessionID+key, config.CacheTTL, nil)
	}
	return chunk
}
