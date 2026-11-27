package model

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/zgsm-ai/chat-rag/internal/types"
)

func TestNewAgentInfoSecurityAndChatLogRedaction(t *testing.T) {
	headers := make(http.Header)
	headers.Set("Authorization", "Bearer super-secret-token")
	headers.Set("X-Apikey", "costrict_team_secret123")
	headers.Set("X-Api-Key", "costrict_team_secret456")
	headers.Set("Cookie", "session=cookie-secret")
	headers.Set("User-Agent", "unknown-client/1.0")

	info := NewAgentInfo(headers, nil)
	if info.ClientType != clientTypeCSC {
		t.Fatalf("ClientType = %q, want %q", info.ClientType, clientTypeCSC)
	}
	if info.ClientTypeSource != "header:x-apikey_prefix" {
		t.Fatalf("unexpected ClientTypeSource: %q", info.ClientTypeSource)
	}

	safeHeaders := CaptureSafeRequestHeaders(headers)
	for _, name := range []string{"Authorization", "X-Apikey", "X-Api-Key", "Cookie"} {
		if _, ok := safeHeaders[name]; ok {
			t.Fatalf("sensitive header %q was captured", name)
		}
	}

	chatLog := ChatLog{
		WorkspaceInfo:  NewWorkspaceInfo(headers),
		AgentInfo:      info,
		Identity:       Identity{AuthToken: headers.Get("Authorization")},
		RequestHeaders: safeHeaders,
	}
	payload, err := json.Marshal(chatLog)
	if err != nil {
		t.Fatalf("marshal chat log: %v", err)
	}
	for _, secret := range []string{"super-secret-token", "Bearer", "costrict_team_secret123", "costrict_team_secret456", "cookie-secret"} {
		if strings.Contains(string(payload), secret) {
			t.Fatalf("serialized ChatLog leaked %q: %s", secret, payload)
		}
	}
}

func TestChatLogJSONPreservesHTMLAndRedactsAuthToken(t *testing.T) {
	chatLog := ChatLog{
		Identity: Identity{AuthToken: "Bearer must-not-leak"},
		ProcessedPrompt: []types.Message{
			{Role: types.RoleUser, Content: "<environment_details>demo</environment_details>"},
		},
	}
	payload, err := chatLog.ToCompressedJSON()
	if err != nil {
		t.Fatalf("serialize chat log: %v", err)
	}
	if !strings.Contains(payload, "<environment_details>") {
		t.Fatalf("ChatLog serialization escaped HTML: %s", payload)
	}
	if strings.Contains(payload, "must-not-leak") {
		t.Fatalf("ChatLog serialization leaked auth token: %s", payload)
	}
}

func TestNewAgentInfoCSBridgeScenarios(t *testing.T) {
	tests := []struct {
		name       string
		headers    map[string]string
		wantClient string
		wantSource string
	}{
		{
			name: "client user agent",
			headers: map[string]string{
				"User-Agent":       "cs-bridge/1.2.3",
				"Client-UserAgent": "csc/4.2.48",
			},
			wantClient: clientTypeCSC,
			wantSource: "header:client-user-agent",
		},
		{
			name: "bridge agent marker without user agent",
			headers: map[string]string{
				"User-Agent":        "cs-bridge/1.2.3",
				"X-CS-Bridge-Agent": "dsh[acp]",
			},
			wantClient: clientTypeDSH,
			wantSource: "header:x-cs-bridge-agent",
		},
		{
			name: "codex build id without user agent",
			headers: map[string]string{
				"User-Agent":       "cs-bridge/1.2.3",
				"X-Codex-Build-Id": "sf-codex",
			},
			wantClient: clientTypeCodex,
			wantSource: "header:x-codex-build-id",
		},
		{
			name: "no source client signal",
			headers: map[string]string{
				"User-Agent": "cs-bridge/1.2.3",
			},
			wantClient: "",
			wantSource: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			headers := make(http.Header)
			for key, value := range tt.headers {
				headers.Set(key, value)
			}
			info := NewAgentInfo(headers, nil)
			if info.ProxyClient != "cs-bridge" || info.ProxyVersion != "1.2.3" {
				t.Fatalf("proxy fields = %q/%q, want cs-bridge/1.2.3", info.ProxyClient, info.ProxyVersion)
			}
			if info.ClientType != tt.wantClient || info.ClientTypeSource != tt.wantSource {
				t.Fatalf("client type/source = %q/%q, want %q/%q", info.ClientType, info.ClientTypeSource, tt.wantClient, tt.wantSource)
			}
		})
	}
}

func TestNewAgentInfoClientTypePriority(t *testing.T) {
	t.Run("zcode user agent wins over codex window header", func(t *testing.T) {
		headers := make(http.Header)
		headers.Set("User-Agent", "ZCode/1.0")
		headers.Set("X-Codex-Window-Id", "window-1")
		info := NewAgentInfo(headers, nil)
		if info.ClientType != clientTypeZCode || info.ClientTypeSource != "header:user-agent" {
			t.Fatalf("client type/source = %q/%q, want ZCode/header:user-agent", info.ClientType, info.ClientTypeSource)
		}
	})

	t.Run("codex header wins over costrict account prefix", func(t *testing.T) {
		headers := make(http.Header)
		headers.Set("X-Codex-Window-Id", "window-1")
		info := NewAgentInfo(headers, &Identity{UserName: "costrict_rd"})
		if info.ClientType != clientTypeCodex || info.ClientTypeSource != "header:x-codex-window-id" {
			t.Fatalf("client type/source = %q/%q, want Codex/header:x-codex-window-id", info.ClientType, info.ClientTypeSource)
		}
	})

	t.Run("api key prefix fallback", func(t *testing.T) {
		headers := make(http.Header)
		headers.Set("X-Apikey", "costrict_team_abc")
		info := NewAgentInfo(headers, nil)
		if info.ClientType != clientTypeCSC || info.ClientTypeSource != "header:x-apikey_prefix" {
			t.Fatalf("client type/source = %q/%q, want CSC/header:x-apikey_prefix", info.ClientType, info.ClientTypeSource)
		}
	})
}

func TestNewAgentInfoParentChildAttribution(t *testing.T) {
	t.Run("csc subagent and main", func(t *testing.T) {
		subHeaders := make(http.Header)
		subHeaders.Set("X-Client-Type", "csc")
		subHeaders.Set("X-Agent-Id", "agent-child")
		subHeaders.Set("X-Parent-Agent-Id", "agent-parent")
		subHeaders.Set("X-Root-Agent-Id", "agent-root")
		subHeaders.Set("X-Parent-Session-Id", "session-parent")
		sub := NewAgentInfo(subHeaders, nil)
		if !sub.IsSubagent || sub.AgentRole != "subagent" {
			t.Fatalf("CSC subagent role = %q/%v, want subagent/true", sub.AgentRole, sub.IsSubagent)
		}
		if sub.AgentType != "csc-subagent" || sub.AgentID != "agent-child" || sub.ParentAgentID != "agent-parent" ||
			sub.RootAgentID != "agent-root" || sub.ParentSessionID != "session-parent" {
			t.Fatalf("unexpected CSC attribution: %+v", sub)
		}

		mainHeaders := make(http.Header)
		mainHeaders.Set("X-Client-Type", "csc")
		main := NewAgentInfo(mainHeaders, nil)
		if main.IsSubagent || main.AgentRole != "main" {
			t.Fatalf("CSC main role = %q/%v, want main/false", main.AgentRole, main.IsSubagent)
		}
	})

	t.Run("codex metadata subagent and compaction", func(t *testing.T) {
		headers := make(http.Header)
		headers.Set("X-Codex-Window-Id", "window-1")
		headers.Set("X-Codex-Turn-Metadata", `{"thread_source":"subagent","request_kind":"compaction","subagent_kind":"review","agent_name":"Reviewer","thread_id":"thread-child"}`)
		headers.Set("X-Codex-Parent-Thread-Id", "thread-parent")
		info := NewAgentInfo(headers, nil)
		if info.ClientType != clientTypeCodex {
			t.Fatalf("ClientType = %q, want Codex", info.ClientType)
		}
		if !info.IsSubagent || info.AgentRole != "subagent" {
			t.Fatalf("Codex role = %q/%v, want subagent/true", info.AgentRole, info.IsSubagent)
		}
		if info.AgentID != "thread-child" || info.AgentName != "Reviewer" || info.RequestKind != "compaction" {
			t.Fatalf("unexpected Codex attribution: %+v", info)
		}
		if info.AgentType != "review" {
			t.Fatalf("AgentType = %q, want review", info.AgentType)
		}
		if info.ParentSessionID != "" {
			t.Fatalf("ParentSessionID = %q, want empty when thread_source is authoritative", info.ParentSessionID)
		}
	})

	t.Run("codex cli thread source prevents parent header misclassification", func(t *testing.T) {
		headers := make(http.Header)
		headers.Set("X-Codex-Window-Id", "window-1")
		headers.Set("X-Codex-Turn-Metadata", `{"thread_source":"cli","request_kind":"compaction"}`)
		headers.Set("X-Codex-Parent-Thread-Id", "thread-parent")
		info := NewAgentInfo(headers, nil)
		if info.IsSubagent || info.AgentRole != "main" {
			t.Fatalf("Codex role = %q/%v, want main/false", info.AgentRole, info.IsSubagent)
		}
		if info.ParentSessionID != "" {
			t.Fatalf("ParentSessionID = %q, want empty", info.ParentSessionID)
		}
		if info.RequestKind != "compaction" {
			t.Fatalf("RequestKind = %q, want compaction", info.RequestKind)
		}
	})

	t.Run("codex parent thread fallback", func(t *testing.T) {
		headers := make(http.Header)
		headers.Set("User-Agent", "codex_cli_rs/0.59.0")
		headers.Set("X-Codex-Parent-Thread-Id", "thread-parent")
		info := NewAgentInfo(headers, nil)
		if !info.IsSubagent || info.AgentRole != "subagent" || info.ParentSessionID != "thread-parent" {
			t.Fatalf("unexpected Codex fallback attribution: %+v", info)
		}
	})

	t.Run("claude code subagent", func(t *testing.T) {
		headers := make(http.Header)
		headers.Set("User-Agent", "claude-cli/1.0")
		headers.Set("X-Claude-Code-Agent-Id", "claude-child")
		info := NewAgentInfo(headers, nil)
		if info.ClientType != clientTypeClaudeCode || info.AgentID != "claude-child" || !info.IsSubagent || info.AgentRole != "subagent" {
			t.Fatalf("unexpected Claude Code attribution: %+v", info)
		}
		if info.AgentType != "cc-subagent" {
			t.Fatalf("AgentType = %q, want cc-subagent", info.AgentType)
		}
	})

	t.Run("dsh does not guess from request headers", func(t *testing.T) {
		headers := make(http.Header)
		headers.Set("User-Agent", "cs-bridge/1.2.3")
		headers.Set("X-CS-Bridge-Agent", "dsh[acp]")
		info := NewAgentInfo(headers, nil)
		if info.ClientType != clientTypeDSH || info.IsSubagent || info.AgentRole != "main" {
			t.Fatalf("unexpected DSH attribution: %+v", info)
		}
	})

	t.Run("opencode subagent via agent type", func(t *testing.T) {
		headers := make(http.Header)
		headers.Set("User-Agent", "opencode/cs-bridge")
		headers.Set("Agent-Type", "opencode-subagent")
		info := NewAgentInfo(headers, nil)
		if info.ClientType != clientTypeOpenCode || !info.IsSubagent || info.AgentRole != "subagent" || info.AgentType != "opencode-subagent" {
			t.Fatalf("unexpected OpenCode attribution: %+v", info)
		}
	})

	t.Run("zcode and pi main agents", func(t *testing.T) {
		zcodeHeaders := make(http.Header)
		zcodeHeaders.Set("User-Agent", "ZCode/1.0")
		zcode := NewAgentInfo(zcodeHeaders, nil)
		if zcode.ClientType != clientTypeZCode || zcode.IsSubagent || zcode.AgentRole != "main" {
			t.Fatalf("unexpected ZCode attribution: %+v", zcode)
		}

		piHeaders := make(http.Header)
		piHeaders.Set("User-Agent", "cs-bridge/1.2.3")
		piHeaders.Set("X-CS-Bridge-Agent", "pi")
		pi := NewAgentInfo(piHeaders, nil)
		if pi.ClientType != clientTypePi || pi.IsSubagent || pi.AgentRole != "main" {
			t.Fatalf("unexpected Pi attribution: %+v", pi)
		}
	})

	t.Run("unknown client keeps unknown role", func(t *testing.T) {
		info := NewAgentInfo(make(http.Header), nil)
		if info.ClientType != "" || info.AgentRole != "unknown" || info.IsSubagent {
			t.Fatalf("unexpected unknown attribution: %+v", info)
		}
	})
}

func TestNewAgentInfoUnifiedHeadersTakePriority(t *testing.T) {
	headers := make(http.Header)
	headers.Set("X-Client-Type", "opencode")
	headers.Set("Agent-Type", "opencode-subagent")
	headers.Set("X-Agent-Id", "unified-agent")
	headers.Set("X-Parent-Agent-Id", "unified-parent")
	headers.Set("X-Root-Agent-Id", "unified-root")
	headers.Set("X-Parent-Session-Id", "unified-parent-session")
	headers.Set("X-Agent-Role", "main")

	info := NewAgentInfo(headers, nil)
	if info.AgentRole != "main" || info.IsSubagent {
		t.Fatalf("unified role was not honored: %+v", info)
	}
	if info.AgentSignalSource != "header:x-agent-role" {
		t.Fatalf("AgentSignalSource = %q, want header:x-agent-role", info.AgentSignalSource)
	}
	if info.AgentID != "unified-agent" || info.ParentAgentID != "unified-parent" ||
		info.RootAgentID != "unified-root" || info.ParentSessionID != "unified-parent-session" {
		t.Fatalf("unified IDs were not preserved: %+v", info)
	}
}

func TestCaptureSafeRequestHeadersWhitelist(t *testing.T) {
	headers := make(http.Header)
	for key, value := range map[string]string{
		"user-agent":              " csc/1 ",
		"client-useragent":        "csc/1",
		"x-agent-id":              "agent",
		"x-codex-turn-metadata":   `{"thread_source":"cli"}`,
		"x-stainless-retry-count": "2",
		"x-costrict-idea":         "idea-1",
		"authorization":           "Bearer secret",
		"x-apikey":                "costrict_team_secret",
		"x-api-key":               "costrict_team_secret",
		"cookie":                  "a=b",
		"x-unrelated":             "nope",
	} {
		headers.Set(key, value)
	}

	safe := CaptureSafeRequestHeaders(headers)
	want := map[string]string{
		"User-Agent":              "csc/1",
		"Client-UserAgent":        "csc/1",
		"X-Agent-Id":              "agent",
		"X-Codex-Turn-Metadata":   `{"thread_source":"cli"}`,
		"X-Stainless-Retry-Count": "2",
		"X-Costrict-Idea":         "idea-1",
	}
	if len(safe) != len(want) {
		t.Fatalf("safe headers = %#v, want %#v", safe, want)
	}
	for key, value := range want {
		if safe[key] != value {
			t.Fatalf("safe[%q] = %q, want %q", key, safe[key], value)
		}
	}
	for _, key := range []string{"Authorization", "X-Apikey", "X-Api-Key", "Cookie", "X-Unrelated"} {
		if _, ok := safe[key]; ok {
			t.Fatalf("unexpected header %q in whitelist snapshot", key)
		}
	}
}

func TestExtractRetryCount(t *testing.T) {
	tests := []struct {
		value string
		want  int
	}{
		{value: "0", want: 0},
		{value: "3", want: 3},
		{value: " 7 ", want: 7},
		{value: "-2", want: 0},
		{value: "bad", want: 0},
		{value: "", want: 0},
	}
	for _, tt := range tests {
		headers := make(http.Header)
		if tt.value != "" {
			headers.Set("X-Stainless-Retry-Count", tt.value)
		}
		if got := ExtractRetryCount(headers); got != tt.want {
			t.Fatalf("ExtractRetryCount(%q) = %d, want %d", tt.value, got, tt.want)
		}
	}
}
