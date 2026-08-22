package executor

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/runtime/executor/helps"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/thinking"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/util"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	openAICompatProtocolOpenAI    = "openai"
	openAICompatProtocolAnthropic = "anthropic"
	openAICompatAuthTypeBearer    = "bearer"
	openAICompatAuthTypeAPIKey    = "x-api-key"
	openAICompatAnthropicVersion  = "2023-06-01"
)

func (e *OpenAICompatExecutor) compatProtocol(auth *cliproxyauth.Auth) string {
	if auth != nil && auth.Attributes != nil {
		if protocol := normalizeOpenAICompatProtocol(auth.Attributes["compat_protocol"]); protocol != "" {
			return protocol
		}
	}
	if compat := e.resolveCompatConfig(auth); compat != nil {
		return normalizeOpenAICompatProtocol(compat.Protocol)
	}
	return openAICompatProtocolOpenAI
}

func (e *OpenAICompatExecutor) compatAuthType(auth *cliproxyauth.Auth) string {
	if auth != nil && auth.Attributes != nil {
		if authType := normalizeOpenAICompatAuthType(auth.Attributes["compat_auth_type"]); authType != "" {
			return authType
		}
	}
	if compat := e.resolveCompatConfig(auth); compat != nil {
		return normalizeOpenAICompatAuthType(compat.AuthType)
	}
	return openAICompatAuthTypeBearer
}

func normalizeOpenAICompatProtocol(protocol string) string {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case openAICompatProtocolAnthropic:
		return openAICompatProtocolAnthropic
	case openAICompatProtocolOpenAI:
		return openAICompatProtocolOpenAI
	default:
		return ""
	}
}

func normalizeOpenAICompatAuthType(authType string) string {
	switch strings.ToLower(strings.TrimSpace(authType)) {
	case openAICompatAuthTypeAPIKey, "api-key", "apikey":
		return openAICompatAuthTypeAPIKey
	case openAICompatAuthTypeBearer:
		return openAICompatAuthTypeBearer
	default:
		return ""
	}
}

func appendOpenAICompatEndpoint(baseURL, endpoint string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if strings.HasSuffix(strings.ToLower(baseURL), strings.ToLower(endpoint)) {
		return baseURL
	}
	return baseURL + endpoint
}

func (e *OpenAICompatExecutor) applyAnthropicCompatHeaders(req *http.Request, auth *cliproxyauth.Auth, apiKey string, stream bool) {
	if req == nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	var attrs map[string]string
	if auth != nil {
		attrs = auth.Attributes
	}
	util.ApplyCustomHeadersFromAttrs(req, attrs)
	if req.Header.Get("Anthropic-Version") == "" {
		req.Header.Set("Anthropic-Version", openAICompatAnthropicVersion)
	}
	if e.compatAuthType(auth) == openAICompatAuthTypeAPIKey {
		if req.Header.Get("x-api-key") == "" && strings.TrimSpace(apiKey) != "" {
			req.Header.Set("x-api-key", apiKey)
		}
	} else if req.Header.Get("Authorization") == "" && req.Header.Get("x-api-key") == "" && strings.TrimSpace(apiKey) != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "cli-proxy-anthropic-compat")
	}
	if stream {
		req.Header.Set("Accept", "text/event-stream")
		req.Header.Set("Cache-Control", "no-cache")
	}
}

func (e *OpenAICompatExecutor) executeAnthropicCompat(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (resp cliproxyexecutor.Response, err error) {
	if opts.Alt == "responses/compact" {
		return resp, statusErr{code: http.StatusNotImplemented, msg: "/responses/compact not supported by Anthropic-compatible providers"}
	}
	if endpointPath := openAICompatImageEndpointPath(opts); endpointPath != "" {
		return resp, statusErr{code: http.StatusBadRequest, msg: "image endpoints not supported by Anthropic-compatible providers"}
	}

	baseModel := thinking.ParseSuffix(req.Model).ModelName
	reporter := helps.NewExecutorUsageReporter(ctx, e, baseModel, auth)
	defer reporter.TrackFailure(ctx, &err)

	baseURL, apiKey := e.resolveCredentials(auth)
	if baseURL == "" {
		return resp, statusErr{code: http.StatusUnauthorized, msg: "missing provider baseURL"}
	}

	from := opts.SourceFormat
	to := sdktranslator.FromString("claude")
	originalPayload := req.Payload
	if len(opts.OriginalRequest) > 0 {
		originalPayload = opts.OriginalRequest
	}
	translateAsStream := opts.Stream || from != to
	originalTranslated := sdktranslator.TranslateRequest(from, to, baseModel, originalPayload, translateAsStream)
	body := sdktranslator.TranslateRequest(from, to, baseModel, req.Payload, translateAsStream)
	body, _ = sjson.SetBytes(body, "model", baseModel)
	if translateAsStream {
		body, _ = sjson.SetBytes(body, "stream", true)
	}
	body, err = thinking.ApplyThinking(body, req.Model, from.String(), to.String(), e.Identifier())
	if err != nil {
		return resp, err
	}

	requestedModel := helps.PayloadRequestedModel(opts, req.Model)
	requestPath := helps.PayloadRequestPath(opts)
	body = helps.ApplyPayloadConfigWithRequest(e.cfg, baseModel, to.String(), from.String(), "", body, originalTranslated, requestedModel, requestPath, opts.Headers)
	body = ensureOpenAICompatAnthropicMaxTokens(body)
	reporter.SetTranslatedReasoningEffort(body, to.String())

	url := appendOpenAICompatEndpoint(baseURL, "/messages")
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return resp, err
	}
	e.applyAnthropicCompatHeaders(httpReq, auth, apiKey, false)
	recordOpenAICompatRequest(ctx, e, auth, url, httpReq, body)

	httpClient := reporter.TrackHTTPClient(helps.NewProxyAwareHTTPClient(ctx, e.cfg, auth, 0))
	httpResp, err := httpClient.Do(httpReq)
	if err != nil {
		helps.RecordAPIResponseError(ctx, e.cfg, err)
		return resp, err
	}
	defer func() {
		if errClose := httpResp.Body.Close(); errClose != nil {
			log.Errorf("openai compat executor: close response body error: %v", errClose)
		}
	}()
	helps.RecordAPIResponseMetadata(ctx, e.cfg, httpResp.StatusCode, httpResp.Header.Clone())
	if httpResp.StatusCode < http.StatusOK || httpResp.StatusCode >= http.StatusMultipleChoices {
		return resp, e.anthropicCompatStatusError(ctx, httpResp)
	}
	bodyOut, err := io.ReadAll(httpResp.Body)
	if err != nil {
		helps.RecordAPIResponseError(ctx, e.cfg, err)
		return resp, err
	}
	helps.AppendAPIResponseChunk(ctx, e.cfg, bodyOut)
	reporter.Publish(ctx, helps.ParseClaudeUsage(bodyOut))
	reporter.EnsurePublished(ctx)
	var param any
	out := sdktranslator.TranslateNonStream(ctx, to, from, req.Model, opts.OriginalRequest, body, bodyOut, &param)
	return cliproxyexecutor.Response{Payload: out, Headers: httpResp.Header.Clone()}, nil
}

func (e *OpenAICompatExecutor) executeAnthropicCompatStream(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (_ *cliproxyexecutor.StreamResult, err error) {
	if endpointPath := openAICompatImageEndpointPath(opts); endpointPath != "" {
		return nil, statusErr{code: http.StatusBadRequest, msg: "image endpoints not supported by Anthropic-compatible providers"}
	}

	baseModel := thinking.ParseSuffix(req.Model).ModelName
	reporter := helps.NewExecutorUsageReporter(ctx, e, baseModel, auth)
	defer reporter.TrackFailure(ctx, &err)

	baseURL, apiKey := e.resolveCredentials(auth)
	if baseURL == "" {
		return nil, statusErr{code: http.StatusUnauthorized, msg: "missing provider baseURL"}
	}

	from := opts.SourceFormat
	to := sdktranslator.FromString("claude")
	originalPayload := req.Payload
	if len(opts.OriginalRequest) > 0 {
		originalPayload = opts.OriginalRequest
	}
	originalTranslated := sdktranslator.TranslateRequest(from, to, baseModel, originalPayload, true)
	body := sdktranslator.TranslateRequest(from, to, baseModel, req.Payload, true)
	body, _ = sjson.SetBytes(body, "model", baseModel)
	body, err = thinking.ApplyThinking(body, req.Model, from.String(), to.String(), e.Identifier())
	if err != nil {
		return nil, err
	}

	requestedModel := helps.PayloadRequestedModel(opts, req.Model)
	requestPath := helps.PayloadRequestPath(opts)
	body = helps.ApplyPayloadConfigWithRequest(e.cfg, baseModel, to.String(), from.String(), "", body, originalTranslated, requestedModel, requestPath, opts.Headers)
	body = ensureOpenAICompatAnthropicMaxTokens(body)
	reporter.SetTranslatedReasoningEffort(body, to.String())

	url := appendOpenAICompatEndpoint(baseURL, "/messages")
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	e.applyAnthropicCompatHeaders(httpReq, auth, apiKey, true)
	recordOpenAICompatRequest(ctx, e, auth, url, httpReq, body)

	httpClient := reporter.TrackHTTPClient(helps.NewProxyAwareHTTPClient(ctx, e.cfg, auth, 0))
	httpResp, err := httpClient.Do(httpReq)
	if err != nil {
		helps.RecordAPIResponseError(ctx, e.cfg, err)
		return nil, err
	}
	helps.RecordAPIResponseMetadata(ctx, e.cfg, httpResp.StatusCode, httpResp.Header.Clone())
	if httpResp.StatusCode < http.StatusOK || httpResp.StatusCode >= http.StatusMultipleChoices {
		statusErr := e.anthropicCompatStatusError(ctx, httpResp)
		if errClose := httpResp.Body.Close(); errClose != nil {
			log.Errorf("openai compat executor: close response body error: %v", errClose)
		}
		return nil, statusErr
	}

	out := make(chan cliproxyexecutor.StreamChunk)
	go func() {
		defer close(out)
		defer func() {
			if errClose := httpResp.Body.Close(); errClose != nil {
				log.Errorf("openai compat executor: close response body error: %v", errClose)
			}
		}()

		scanner := bufio.NewScanner(httpResp.Body)
		scanner.Buffer(nil, 52_428_800)
		var param any
		for scanner.Scan() {
			line := scanner.Bytes()
			helps.AppendAPIResponseChunk(ctx, e.cfg, line)
			if detail, ok := helps.ParseClaudeStreamUsage(line); ok {
				reporter.Publish(ctx, detail)
			}
			trimmed := bytes.TrimSpace(line)
			if len(trimmed) == 0 || bytes.HasPrefix(trimmed, []byte("event:")) || bytes.HasPrefix(trimmed, []byte(":")) || bytes.HasPrefix(trimmed, []byte("id:")) || bytes.HasPrefix(trimmed, []byte("retry:")) {
				continue
			}
			if !bytes.HasPrefix(trimmed, []byte("data:")) {
				if bytes.HasPrefix(trimmed, []byte("{")) || bytes.HasPrefix(trimmed, []byte("[")) {
					streamErr := statusErr{code: http.StatusBadGateway, msg: string(trimmed)}
					helps.RecordAPIResponseError(ctx, e.cfg, streamErr)
					reporter.PublishFailure(ctx, streamErr)
					select {
					case out <- cliproxyexecutor.StreamChunk{Err: streamErr}:
					case <-ctx.Done():
					}
					return
				}
				continue
			}
			chunks := sdktranslator.TranslateStream(ctx, to, from, req.Model, opts.OriginalRequest, body, bytes.Clone(trimmed), &param)
			for i := range chunks {
				select {
				case out <- cliproxyexecutor.StreamChunk{Payload: chunks[i]}:
				case <-ctx.Done():
					return
				}
			}
		}
		if errScan := scanner.Err(); errScan != nil {
			helps.RecordAPIResponseError(ctx, e.cfg, errScan)
			reporter.PublishFailure(ctx, errScan)
			select {
			case out <- cliproxyexecutor.StreamChunk{Err: errScan}:
			case <-ctx.Done():
			}
		}
		reporter.EnsurePublished(ctx)
	}()
	return &cliproxyexecutor.StreamResult{Headers: httpResp.Header.Clone(), Chunks: out}, nil
}

func ensureOpenAICompatAnthropicMaxTokens(body []byte) []byte {
	body = ensureModelMaxTokens(body, "")
	if len(body) == 0 || !gjson.ValidBytes(body) || gjson.GetBytes(body, "max_tokens").Exists() {
		return body
	}
	body, _ = sjson.SetBytes(body, "max_tokens", defaultModelMaxTokens)
	return body
}

func (e *OpenAICompatExecutor) anthropicCompatStatusError(ctx context.Context, httpResp *http.Response) error {
	body, readErr := io.ReadAll(httpResp.Body)
	if readErr != nil {
		helps.RecordAPIResponseError(ctx, e.cfg, readErr)
		body = []byte(readErr.Error())
	}
	helps.AppendAPIResponseChunk(ctx, e.cfg, body)
	helps.LogWithRequestID(ctx).Debugf("request error, error status: %d, error message: %s", httpResp.StatusCode, helps.SummarizeErrorBody(httpResp.Header.Get("Content-Type"), body))
	statusErr := statusErr{code: httpResp.StatusCode, msg: string(body)}
	if httpResp.StatusCode == http.StatusTooManyRequests {
		statusErr.retryAfter = parseAnthropicRetryAfter(httpResp.Header)
	}
	return statusErr
}

func recordOpenAICompatRequest(ctx context.Context, executor *OpenAICompatExecutor, auth *cliproxyauth.Auth, url string, httpReq *http.Request, body []byte) {
	var authID, authLabel, authType, authValue string
	if auth != nil {
		authID = auth.ID
		authLabel = auth.Label
		authType, authValue = auth.AccountInfo()
	}
	helps.RecordAPIRequest(ctx, executor.cfg, helps.UpstreamRequestLog{
		URL:       url,
		Method:    http.MethodPost,
		Headers:   httpReq.Header.Clone(),
		Body:      body,
		Provider:  executor.Identifier(),
		AuthID:    authID,
		AuthLabel: authLabel,
		AuthType:  authType,
		AuthValue: authValue,
	})
}
