package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

const (
	defaultProxyProbeTimeout          = 10 * time.Second
	defaultProxyProbeResponseMaxBytes = int64(1024 * 1024)
	proxyQualityRequestTimeout        = 15 * time.Second
	proxyQualityMaxBodyBytes          = int64(8 * 1024)
	proxyQualityClientUserAgent       = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/136.0.0.0 Safari/537.36"
)

// ProxyExitInfo is the exit IP / geo result from a connectivity probe.
type ProxyExitInfo struct {
	IP          string
	City        string
	Region      string
	Country     string
	CountryCode string
}

// ProxyTestResult is the admin connectivity test response.
type ProxyTestResult struct {
	Success     bool   `json:"success"`
	Message     string `json:"message"`
	LatencyMs   int64  `json:"latency_ms"`
	IPAddress   string `json:"ip_address,omitempty"`
	City        string `json:"city,omitempty"`
	Region      string `json:"region,omitempty"`
	Country     string `json:"country,omitempty"`
	CountryCode string `json:"country_code,omitempty"`
}

// ProxyQualityCheckItem is one target check inside a quality report.
type ProxyQualityCheckItem struct {
	Target     string `json:"target"`
	Status     string `json:"status"` // pass | warn | fail | challenge
	LatencyMs  int64  `json:"latency_ms,omitempty"`
	HTTPStatus int    `json:"http_status,omitempty"`
	Message    string `json:"message,omitempty"`
	CFRay      string `json:"cf_ray,omitempty"`
}

// ProxyQualityCheckResult is the multi-target quality report.
type ProxyQualityCheckResult struct {
	ProxyID        int                     `json:"proxy_id"`
	Success        bool                    `json:"success"`
	Score          int                     `json:"score"`
	Grade          string                  `json:"grade"`
	Summary        string                  `json:"summary"`
	Status         string                  `json:"status"` // healthy | warn | failed | challenge
	CheckedAt      int64                   `json:"checked_at"`
	ExitIP         string                  `json:"exit_ip,omitempty"`
	Country        string                  `json:"country,omitempty"`
	CountryCode    string                  `json:"country_code,omitempty"`
	BaseLatencyMs  int64                   `json:"base_latency_ms,omitempty"`
	PassedCount    int                     `json:"passed_count"`
	WarnCount      int                     `json:"warn_count"`
	FailedCount    int                     `json:"failed_count"`
	ChallengeCount int                     `json:"challenge_count"`
	Items          []ProxyQualityCheckItem `json:"items"`
}

type proxyQualityTarget struct {
	Target          string
	URL             string
	Method          string
	AllowedStatuses map[int]struct{}
}

var proxyQualityTargets = []proxyQualityTarget{
	{
		Target: "openai",
		URL:    "https://api.openai.com/v1/models",
		Method: http.MethodGet,
		AllowedStatuses: map[int]struct{}{
			http.StatusUnauthorized: {},
		},
	},
	{
		Target: "anthropic",
		URL:    "https://api.anthropic.com/v1/messages",
		Method: http.MethodGet,
		AllowedStatuses: map[int]struct{}{
			http.StatusUnauthorized:     {},
			http.StatusMethodNotAllowed: {},
			http.StatusNotFound:         {},
			http.StatusBadRequest:       {},
		},
	},
	{
		Target: "gemini",
		URL:    "https://generativelanguage.googleapis.com/$discovery/rest?version=v1beta",
		Method: http.MethodGet,
		AllowedStatuses: map[int]struct{}{
			http.StatusOK: {},
		},
	},
	{
		Target: "grok",
		URL:    "https://api.x.ai/v1/models",
		Method: http.MethodGet,
		AllowedStatuses: map[int]struct{}{
			http.StatusUnauthorized: {},
		},
	},
}

var probeURLs = []struct {
	url    string
	parser string
}{
	{"http://ip-api.com/json/?lang=zh-CN", "ip-api"},
	{"http://api64.ipify.org?format=json", "ipify"},
}

var (
	cfRayPattern  = regexp.MustCompile(`(?i)cf-ray[:\s=]+([a-z0-9-]+)`)
	cRayPattern   = regexp.MustCompile(`(?i)cRay:\s*'([a-z0-9-]+)'`)
	htmlChallenge = []string{
		"window._cf_chl_opt",
		"just a moment",
		"enable javascript and cookies to continue",
		"__cf_chl_",
		"challenge-platform",
	}
)

// ProxyQualityGrade maps a 0–100 score to A–F bands (ported from sub2api).
func ProxyQualityGrade(score int) string {
	switch {
	case score >= 90:
		return "A"
	case score >= 75:
		return "B"
	case score >= 60:
		return "C"
	case score >= 40:
		return "D"
	default:
		return "F"
	}
}

// FinalizeProxyQualityResult fills Score, Grade, Summary, Status, and Success.
func FinalizeProxyQualityResult(result *ProxyQualityCheckResult) {
	if result == nil {
		return
	}
	score := 100 - result.WarnCount*10 - result.FailedCount*22 - result.ChallengeCount*30
	if score < 0 {
		score = 0
	}
	result.Score = score
	result.Grade = ProxyQualityGrade(score)
	result.Summary = fmt.Sprintf(
		"通过 %d 项，告警 %d 项，失败 %d 项，挑战 %d 项",
		result.PassedCount,
		result.WarnCount,
		result.FailedCount,
		result.ChallengeCount,
	)
	result.Status = proxyQualityOverallStatus(result)
	result.Success = result.FailedCount == 0 && result.ChallengeCount == 0
}

func proxyQualityOverallStatus(result *ProxyQualityCheckResult) string {
	if result == nil {
		return ""
	}
	if result.ChallengeCount > 0 {
		return "challenge"
	}
	if result.FailedCount > 0 {
		return "failed"
	}
	if result.WarnCount > 0 {
		return "warn"
	}
	if result.PassedCount > 0 {
		return "healthy"
	}
	return "failed"
}

// ProbeProxyExit measures connectivity through proxyURL and returns exit info + latency.
func ProbeProxyExit(ctx context.Context, proxyURL string) (*ProxyExitInfo, int64, error) {
	client, err := GetHttpClientWithProxy(proxyURL)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create proxy client: %w", err)
	}
	// Use a timeout-bound client for probes without mutating the shared cache entry.
	probeClient := &http.Client{
		Transport: client.Transport,
		Timeout:   defaultProxyProbeTimeout,
	}

	var lastErr error
	for _, probe := range probeURLs {
		exitInfo, latencyMs, err := probeWithURL(ctx, probeClient, probe.url, probe.parser)
		if err == nil {
			return exitInfo, latencyMs, nil
		}
		lastErr = err
	}
	return nil, 0, fmt.Errorf("all probe URLs failed, last error: %w", lastErr)
}

func probeWithURL(ctx context.Context, client *http.Client, url string, parser string) (*ProxyExitInfo, int64, error) {
	startTime := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("proxy connection failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	latencyMs := time.Since(startTime).Milliseconds()
	if resp.StatusCode != http.StatusOK {
		return nil, latencyMs, fmt.Errorf("request failed with status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, defaultProxyProbeResponseMaxBytes+1))
	if err != nil {
		return nil, latencyMs, fmt.Errorf("failed to read response: %w", err)
	}
	if int64(len(body)) > defaultProxyProbeResponseMaxBytes {
		return nil, latencyMs, fmt.Errorf("proxy probe response exceeds limit: %d", defaultProxyProbeResponseMaxBytes)
	}

	switch parser {
	case "ip-api":
		return parseIPAPI(body, latencyMs)
	case "ipify":
		return parseIPify(body, latencyMs)
	default:
		return nil, latencyMs, fmt.Errorf("unknown parser: %s", parser)
	}
}

func parseIPAPI(body []byte, latencyMs int64) (*ProxyExitInfo, int64, error) {
	var ipInfo struct {
		Status      string `json:"status"`
		Message     string `json:"message"`
		Query       string `json:"query"`
		City        string `json:"city"`
		Region      string `json:"region"`
		RegionName  string `json:"regionName"`
		Country     string `json:"country"`
		CountryCode string `json:"countryCode"`
	}
	if err := common.Unmarshal(body, &ipInfo); err != nil {
		preview := string(body)
		if len(preview) > 200 {
			preview = preview[:200] + "..."
		}
		return nil, latencyMs, fmt.Errorf("failed to parse response: %w (body: %s)", err, preview)
	}
	if strings.ToLower(ipInfo.Status) != "success" {
		if ipInfo.Message == "" {
			ipInfo.Message = "ip-api request failed"
		}
		return nil, latencyMs, fmt.Errorf("ip-api request failed: %s", ipInfo.Message)
	}
	region := ipInfo.RegionName
	if region == "" {
		region = ipInfo.Region
	}
	return &ProxyExitInfo{
		IP:          ipInfo.Query,
		City:        ipInfo.City,
		Region:      region,
		Country:     ipInfo.Country,
		CountryCode: ipInfo.CountryCode,
	}, latencyMs, nil
}

func parseIPify(body []byte, latencyMs int64) (*ProxyExitInfo, int64, error) {
	var result struct {
		IP string `json:"ip"`
	}
	if err := common.Unmarshal(body, &result); err != nil {
		return nil, latencyMs, fmt.Errorf("failed to parse ipify response: %w", err)
	}
	if result.IP == "" {
		return nil, latencyMs, fmt.Errorf("ipify: no IP found in response")
	}
	return &ProxyExitInfo{IP: result.IP}, latencyMs, nil
}

// TestManagedProxy loads a proxy by id, probes exit connectivity, and persists results.
// Network failures return Success=false without a Go error.
func TestManagedProxy(ctx context.Context, id int) (*ProxyTestResult, error) {
	proxy, err := model.GetProxyById(id)
	if err != nil {
		return nil, err
	}

	exitInfo, latencyMs, err := ProbeProxyExit(ctx, proxy.URL())
	if err != nil {
		proxy.LatencyMs = 0
		proxy.LatencyStatus = "failed"
		proxy.LatencyMessage = err.Error()
		_ = proxy.UpdateProbeResult()
		return &ProxyTestResult{
			Success: false,
			Message: err.Error(),
		}, nil
	}

	proxy.LatencyMs = latencyMs
	proxy.LatencyStatus = "success"
	proxy.LatencyMessage = "Proxy is accessible"
	proxy.IpAddress = exitInfo.IP
	proxy.City = exitInfo.City
	proxy.Region = exitInfo.Region
	proxy.Country = exitInfo.Country
	proxy.CountryCode = exitInfo.CountryCode
	_ = proxy.UpdateProbeResult()

	return &ProxyTestResult{
		Success:     true,
		Message:     "Proxy is accessible",
		LatencyMs:   latencyMs,
		IPAddress:   exitInfo.IP,
		City:        exitInfo.City,
		Region:      exitInfo.Region,
		Country:     exitInfo.Country,
		CountryCode: exitInfo.CountryCode,
	}, nil
}

// CheckManagedProxyQuality runs base connectivity + multi-target quality checks.
func CheckManagedProxyQuality(ctx context.Context, id int) (*ProxyQualityCheckResult, error) {
	proxy, err := model.GetProxyById(id)
	if err != nil {
		return nil, err
	}

	result := &ProxyQualityCheckResult{
		ProxyID:   id,
		Score:     100,
		Grade:     "A",
		CheckedAt: time.Now().Unix(),
		Items:     make([]ProxyQualityCheckItem, 0, len(proxyQualityTargets)+1),
	}

	proxyURL := proxy.URL()
	exitInfo, latencyMs, err := ProbeProxyExit(ctx, proxyURL)
	if err != nil {
		result.Items = append(result.Items, ProxyQualityCheckItem{
			Target:    "base_connectivity",
			Status:    "fail",
			LatencyMs: latencyMs,
			Message:   err.Error(),
		})
		result.FailedCount++
		FinalizeProxyQualityResult(result)
		persistProxyQuality(proxy, result, nil)
		return result, nil
	}

	result.ExitIP = exitInfo.IP
	result.Country = exitInfo.Country
	result.CountryCode = exitInfo.CountryCode
	result.BaseLatencyMs = latencyMs
	result.Items = append(result.Items, ProxyQualityCheckItem{
		Target:    "base_connectivity",
		Status:    "pass",
		LatencyMs: latencyMs,
		Message:   "代理出口连通正常",
	})
	result.PassedCount++

	client, err := GetHttpClientWithProxy(proxyURL)
	if err != nil {
		result.Items = append(result.Items, ProxyQualityCheckItem{
			Target:  "http_client",
			Status:  "fail",
			Message: fmt.Sprintf("创建检测客户端失败: %v", err),
		})
		result.FailedCount++
		FinalizeProxyQualityResult(result)
		persistProxyQuality(proxy, result, exitInfo)
		return result, nil
	}
	qualityClient := &http.Client{
		Transport: client.Transport,
		Timeout:   proxyQualityRequestTimeout,
	}

	for _, target := range proxyQualityTargets {
		item := runProxyQualityTarget(ctx, qualityClient, target)
		result.Items = append(result.Items, item)
		switch item.Status {
		case "pass":
			result.PassedCount++
		case "warn":
			result.WarnCount++
		case "challenge":
			result.ChallengeCount++
		default:
			result.FailedCount++
		}
	}

	FinalizeProxyQualityResult(result)
	persistProxyQuality(proxy, result, exitInfo)
	return result, nil
}

func runProxyQualityTarget(ctx context.Context, client *http.Client, target proxyQualityTarget) ProxyQualityCheckItem {
	item := ProxyQualityCheckItem{Target: target.Target}

	req, err := http.NewRequestWithContext(ctx, target.Method, target.URL, nil)
	if err != nil {
		item.Status = "fail"
		item.Message = fmt.Sprintf("构建请求失败: %v", err)
		return item
	}
	req.Header.Set("Accept", "application/json,text/html,*/*")
	req.Header.Set("User-Agent", proxyQualityClientUserAgent)

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		item.Status = "fail"
		item.LatencyMs = time.Since(start).Milliseconds()
		item.Message = fmt.Sprintf("请求失败: %v", err)
		return item
	}
	defer func() { _ = resp.Body.Close() }()
	item.LatencyMs = time.Since(start).Milliseconds()
	item.HTTPStatus = resp.StatusCode

	body, readErr := io.ReadAll(io.LimitReader(resp.Body, proxyQualityMaxBodyBytes+1))
	if readErr != nil {
		item.Status = "fail"
		item.Message = fmt.Sprintf("读取响应失败: %v", readErr)
		return item
	}
	if int64(len(body)) > proxyQualityMaxBodyBytes {
		body = body[:proxyQualityMaxBodyBytes]
	}

	if isCloudflareChallengeResponse(resp.StatusCode, resp.Header, body) {
		item.Status = "challenge"
		item.CFRay = extractCloudflareRayID(resp.Header, body)
		item.Message = "命中 Cloudflare challenge"
		return item
	}

	if _, ok := target.AllowedStatuses[resp.StatusCode]; ok {
		item.Status = "pass"
		if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
			item.Message = fmt.Sprintf("HTTP %d", resp.StatusCode)
		} else {
			item.Message = fmt.Sprintf("HTTP %d（目标可达）", resp.StatusCode)
		}
		return item
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		item.Status = "warn"
		item.Message = "目标返回 429，可能存在频控"
		return item
	}

	item.Status = "fail"
	item.Message = fmt.Sprintf("非预期状态码: %d", resp.StatusCode)
	return item
}

func persistProxyQuality(proxy *model.Proxy, result *ProxyQualityCheckResult, exitInfo *ProxyExitInfo) {
	if proxy == nil || result == nil {
		return
	}
	proxy.QualityStatus = result.Status
	proxy.QualityScore = result.Score
	proxy.QualityGrade = result.Grade
	proxy.QualitySummary = result.Summary
	proxy.QualityChecked = result.CheckedAt
	if result.BaseLatencyMs > 0 {
		proxy.LatencyMs = result.BaseLatencyMs
	}
	basePass := false
	for _, item := range result.Items {
		if item.Target == "base_connectivity" {
			basePass = item.Status == "pass"
			break
		}
	}
	if basePass {
		proxy.LatencyStatus = "success"
		proxy.LatencyMessage = "Proxy is accessible"
	} else {
		proxy.LatencyStatus = "failed"
		if result.Summary != "" {
			proxy.LatencyMessage = result.Summary
		}
	}
	if exitInfo != nil {
		proxy.IpAddress = exitInfo.IP
		proxy.Country = exitInfo.Country
		proxy.CountryCode = exitInfo.CountryCode
		proxy.Region = exitInfo.Region
		proxy.City = exitInfo.City
	}
	_ = proxy.UpdateProbeResult()
}

func isCloudflareChallengeResponse(statusCode int, headers http.Header, body []byte) bool {
	if statusCode != http.StatusForbidden && statusCode != http.StatusTooManyRequests {
		return false
	}
	if headers != nil && strings.EqualFold(strings.TrimSpace(headers.Get("cf-mitigated")), "challenge") {
		return true
	}
	preview := strings.ToLower(truncateBody(body, 4096))
	for _, marker := range htmlChallenge {
		if strings.Contains(preview, marker) {
			return true
		}
	}
	contentType := ""
	if headers != nil {
		contentType = strings.ToLower(strings.TrimSpace(headers.Get("content-type")))
	}
	if strings.Contains(contentType, "text/html") &&
		(strings.Contains(preview, "<html") || strings.Contains(preview, "<!doctype html")) &&
		(strings.Contains(preview, "cloudflare") || strings.Contains(preview, "challenge")) {
		return true
	}
	return false
}

func extractCloudflareRayID(headers http.Header, body []byte) string {
	if headers != nil {
		if rayID := strings.TrimSpace(headers.Get("cf-ray")); rayID != "" {
			return rayID
		}
	}
	preview := truncateBody(body, 8192)
	if matches := cfRayPattern.FindStringSubmatch(preview); len(matches) >= 2 {
		return strings.TrimSpace(matches[1])
	}
	if matches := cRayPattern.FindStringSubmatch(preview); len(matches) >= 2 {
		return strings.TrimSpace(matches[1])
	}
	return ""
}

func truncateBody(body []byte, max int) string {
	if len(body) <= max {
		return string(body)
	}
	return string(body[:max])
}
