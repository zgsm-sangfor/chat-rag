package service

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/zgsm-ai/chat-rag/internal/model"
)

func TestBuildResponseMetrics_UpstreamTraceID(t *testing.T) {
	reporter := NewChatMetricsReporter("", "", "X-Custom-Trace-ID")
	chatLog := &model.ChatLog{
		ResponseHeaders: []map[string]string{
			{"x-custom-trace-id": "trace-old"},
			{"X-Custom-Trace-Id": "trace-final"},
		},
	}

	metrics := reporter.buildResponseMetrics(chatLog, nil)
	if metrics.UpstreamTraceID != "trace-final" {
		t.Fatalf("UpstreamTraceID = %q, want %q", metrics.UpstreamTraceID, "trace-final")
	}

	payload, err := json.Marshal(struct {
		ResponseMetrics ResponseMetrics `json:"response_metrics"`
	}{ResponseMetrics: metrics})
	if err != nil {
		t.Fatalf("marshal metrics: %v", err)
	}
	if string(payload) == "" || !containsJSONField(payload, "upstream_trace_id", "trace-final") {
		t.Fatalf("payload does not contain response_metrics.upstream_trace_id: %s", payload)
	}
}

func TestBuildRequestMetrics_RetryNumFromRequestHeaders(t *testing.T) {
	reporter := NewChatMetricsReporter("", "", "")
	chatLog := &model.ChatLog{
		RequestHeaders: map[string]string{
			"x-stainless-retry-count": "3",
		},
	}

	metrics := reporter.buildRequestMetrics(chatLog)
	if metrics.RetryNum != 3 {
		t.Fatalf("RetryNum = %d, want 3", metrics.RetryNum)
	}
}

func TestMetricsReport_AgentInfoAndCredentialSafety(t *testing.T) {
	headers := make(http.Header)
	headers.Set("Authorization", "Bearer report-secret")
	headers.Set("X-Apikey", "costrict_team_report_secret")
	headers.Set("Cookie", "sid=report-cookie-secret")
	headers.Set("User-Agent", "csc/4.2.48")
	headers.Set("X-Stainless-Retry-Count", "4")

	chatLog := &model.ChatLog{
		AgentInfo:      model.NewAgentInfo(headers, nil),
		Identity:       model.Identity{AuthToken: headers.Get("Authorization")},
		RequestHeaders: model.CaptureSafeRequestHeaders(headers),
	}

	reporter := NewChatMetricsReporter("", "", "")
	report := reporter.convertChatLogToReport(chatLog, nil)
	payload, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}

	if report.AgentInfo == nil || report.AgentInfo.ClientType != "CSC" {
		t.Fatalf("AgentInfo = %+v, want CSC", report.AgentInfo)
	}
	if report.RequestMetrics.RetryNum != 4 {
		t.Fatalf("RetryNum = %d, want 4", report.RequestMetrics.RetryNum)
	}
	if strings.Contains(string(payload), "request_headers") {
		t.Fatalf("metrics report must not include request_headers: %s", payload)
	}
	for _, secret := range []string{"report-secret", "Bearer", "costrict_team_report_secret", "report-cookie-secret"} {
		if strings.Contains(string(payload), secret) {
			t.Fatalf("metrics report leaked %q: %s", secret, payload)
		}
	}
}

func TestBuildResponseMetrics_UpstreamTraceIDDisabled(t *testing.T) {
	reporter := NewChatMetricsReporter("", "", "")
	chatLog := &model.ChatLog{
		ResponseHeaders: []map[string]string{{"x-oneapi-request-id": "trace-id"}},
	}

	metrics := reporter.buildResponseMetrics(chatLog, nil)
	if metrics.UpstreamTraceID != "" {
		t.Fatalf("UpstreamTraceID = %q, want empty", metrics.UpstreamTraceID)
	}
}

func containsJSONField(payload []byte, name, want string) bool {
	var decoded struct {
		ResponseMetrics map[string]any `json:"response_metrics"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return false
	}
	got, ok := decoded.ResponseMetrics[name].(string)
	return ok && got == want
}
