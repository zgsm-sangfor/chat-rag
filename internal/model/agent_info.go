package model

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

const (
	clientTypeCSC        = "CSC"
	clientTypeCodex      = "Codex"
	clientTypeClaudeCode = "Claude Code"
	clientTypeDSH        = "DSH"
	clientTypeZCode      = "ZCode"
	clientTypeOpenCode   = "OpenCode"
	clientTypePi         = "Pi"
)

// AgentInfo stores normalized client, proxy, and parent/sub-agent attribution
// extracted from incoming request headers.
type AgentInfo struct {
	ProxyClient  string `json:"proxy_client,omitempty"`
	ProxyVersion string `json:"proxy_version,omitempty"`

	ClientType       string `json:"client_type,omitempty"`
	ClientTypeSource string `json:"client_type_source,omitempty"`

	AgentType         string `json:"agent_type,omitempty"`
	AgentName         string `json:"agent_name,omitempty"`
	AgentID           string `json:"agent_id,omitempty"`
	ParentAgentID     string `json:"parent_agent_id,omitempty"`
	RootAgentID       string `json:"root_agent_id,omitempty"`
	ParentSessionID   string `json:"parent_session_id,omitempty"`
	AgentRole         string `json:"agent_role,omitempty"`
	IsSubagent        bool   `json:"is_subagent,omitempty"`
	AgentSignalSource string `json:"agent_signal_source,omitempty"`
	RequestKind       string `json:"request_kind,omitempty"`
}

// safeRequestHeaderNames is deliberately explicit: only headers needed to
// reconstruct client, proxy, agent, session and retry attribution are logged.
var safeRequestHeaderNames = []string{
	"User-Agent",
	"Client-UserAgent",
	"X-CS-Bridge-Agent",
	"X-Codex-Build-Id",
	"X-Client-Type",
	"X-Known-Client",
	"X-Costrict-Version",
	"X-Agent-Id",
	"X-Parent-Agent-Id",
	"X-Root-Agent-Id",
	"X-Parent-Session-Id",
	"X-Agent-Role",
	"Agent-Type",
	"X-Claude-Code-Agent-Id",
	"X-Codex-Window-Id",
	"X-Codex-Turn-Metadata",
	"X-Codex-Parent-Thread-Id",
	"X-Openai-Subagent",
	"X-Opencode-Session",
	"Thread-Id",
	"X-Costrict-Idea",
	"X-Session-Id",
	"X-Claude-Code-Session-Id",
	"X-Deepseek-Harness-Session-Id",
	"Session-Id",
	"X-Stainless-Retry-Count",
}

// NewAgentInfo normalizes the source client and extracts proxy and agent
// attribution from request headers. Sensitive headers are never copied into the
// returned value; API key prefixes are inspected in memory only.
func NewAgentInfo(headers http.Header, identity *Identity) *AgentInfo {
	info := &AgentInfo{}

	rawUA := headerValue(headers, "User-Agent")
	clientUA := headerValue(headers, "Client-UserAgent")
	effectiveUA := rawUA
	effectiveUASource := "header:user-agent"

	if version, ok := csBridgeProductVersion(rawUA); ok {
		info.ProxyClient = "cs-bridge"
		info.ProxyVersion = version
		effectiveUA = clientUA
		effectiveUASource = "header:client-user-agent"
	} else if effectiveUA == "" {
		effectiveUA = clientUA
		effectiveUASource = "header:client-user-agent"
	}

	info.ClientType, info.ClientTypeSource = resolveClientType(headers, identity, effectiveUA, effectiveUASource)
	populateAgentAttribution(info, headers)

	return info
}

// CaptureSafeRequestHeaders returns a case-normalized snapshot containing only
// non-sensitive whitelisted request headers.
func CaptureSafeRequestHeaders(headers http.Header) map[string]string {
	safe := make(map[string]string)
	for _, name := range safeRequestHeaderNames {
		if value := headerValue(headers, name); value != "" {
			safe[name] = value
		}
	}
	return safe
}

// ExtractRetryCount parses X-Stainless-Retry-Count. Missing, malformed or
// negative values are treated as zero.
func ExtractRetryCount(headers http.Header) int {
	raw := headerValue(headers, "X-Stainless-Retry-Count")
	if raw == "" {
		return 0
	}
	count, err := strconv.Atoi(raw)
	if err != nil || count < 0 {
		return 0
	}
	return count
}

func resolveClientType(headers http.Header, identity *Identity, effectiveUA, effectiveUASource string) (string, string) {
	// 1. Explicit client identity header.
	if value := headerValue(headers, "X-Client-Type"); value != "" {
		if normalized, ok := normalizeClientType(value); ok {
			return normalized, "header:x-client-type"
		}
		return value, "header:x-client-type"
	}

	// 2. cs-bridge managed-client markers.
	if value := headerValue(headers, "X-CS-Bridge-Agent"); value != "" {
		if normalized, ok := normalizeClientType(value); ok {
			return normalized, "header:x-cs-bridge-agent"
		}
	}
	if headerValue(headers, "X-Codex-Build-Id") != "" {
		return clientTypeCodex, "header:x-codex-build-id"
	}

	// 3. Legacy explicit client header.
	if value := headerValue(headers, "X-Known-Client"); value != "" {
		if normalized, ok := normalizeClientType(value); ok {
			return normalized, "header:x-known-client"
		}
	}

	// 4. Effective source-client UA. ZCode must precede the Codex headers below
	// because ZCode can also carry X-Codex-Window-Id.
	if clientType, ok := clientTypeFromUserAgent(effectiveUA); ok {
		return clientType, effectiveUASource
	}

	// 5. Client-specific feature headers, mainly for clients without a UA.
	if headerValue(headers, "X-Codex-Window-Id") != "" {
		return clientTypeCodex, "header:x-codex-window-id"
	}
	if headerValue(headers, "X-Codex-Turn-Metadata") != "" {
		return clientTypeCodex, "header:x-codex-turn-metadata"
	}
	if headerValue(headers, "X-Opencode-Session") != "" {
		return clientTypeOpenCode, "header:x-opencode-session"
	}
	if headerValue(headers, "X-Claude-Code-Agent-Id") != "" {
		return clientTypeClaudeCode, "header:x-claude-code-agent-id"
	}

	// 6. Account and API-key prefix fallbacks. The values are read in memory and
	// never returned or logged.
	if identity != nil && strings.HasPrefix(strings.TrimSpace(identity.UserName), "costrict_") {
		return clientTypeCSC, "identity:username_prefix"
	}
	if strings.HasPrefix(headerValue(headers, "X-Apikey"), "costrict_team_") {
		return clientTypeCSC, "header:x-apikey_prefix"
	}
	if strings.HasPrefix(headerValue(headers, "X-Api-Key"), "costrict_team_") {
		return clientTypeCSC, "header:x-api-key-prefix"
	}

	return "", ""
}

func populateAgentAttribution(info *AgentInfo, headers http.Header) {
	info.AgentType = headerValue(headers, "Agent-Type")
	info.AgentID = headerValue(headers, "X-Agent-Id")
	info.ParentAgentID = headerValue(headers, "X-Parent-Agent-Id")
	info.RootAgentID = headerValue(headers, "X-Root-Agent-Id")
	info.ParentSessionID = headerValue(headers, "X-Parent-Session-Id")

	roleDecided := false
	switch role := strings.ToLower(headerValue(headers, "X-Agent-Role")); role {
	case "main", "subagent":
		setAgentRole(info, role, "header:x-agent-role")
		roleDecided = true
	default:
		if info.ParentAgentID != "" {
			setAgentRole(info, "subagent", "header:x-parent-agent-id")
			roleDecided = true
		} else if info.ParentSessionID != "" {
			setAgentRole(info, "subagent", "header:x-parent-session-id")
			roleDecided = true
		}
	}

	switch info.ClientType {
	case clientTypeCSC:
		if !roleDecided {
			switch {
			case info.AgentID != "":
				setAgentRole(info, "subagent", "header:x-agent-id")
				roleDecided = true
			case info.ParentAgentID != "":
				setAgentRole(info, "subagent", "header:x-parent-agent-id")
				roleDecided = true
			}
		}
		if info.IsSubagent {
			setAgentTypeIfEmpty(info, "csc-subagent")
		}

	case clientTypeCodex:
		populateCodexAttribution(info, headers, &roleDecided)
		if info.IsSubagent {
			setAgentTypeIfEmpty(info, "codex-subagent")
		}

	case clientTypeClaudeCode:
		claudeAgentID := headerValue(headers, "X-Claude-Code-Agent-Id")
		if claudeAgentID != "" {
			if info.AgentID == "" {
				info.AgentID = claudeAgentID
			}
			if !roleDecided {
				setAgentRole(info, "subagent", "header:x-claude-code-agent-id")
				roleDecided = true
			}
		}
		if info.IsSubagent {
			setAgentTypeIfEmpty(info, "cc-subagent")
		}
	}

	// Generic fallbacks are used only when neither unified nor client-specific
	// evidence has already decided the role.
	if !roleDecided && strings.HasSuffix(strings.ToLower(info.AgentType), "-subagent") {
		setAgentRole(info, "subagent", "header:agent-type")
		roleDecided = true
	}
	if !roleDecided && isKnownClientType(info.ClientType) {
		setAgentRole(info, "main", "client:main")
		roleDecided = true
	}
	if !roleDecided {
		info.AgentRole = "unknown"
		info.AgentSignalSource = "default:unknown"
	}
}

func populateCodexAttribution(info *AgentInfo, headers http.Header, roleDecided *bool) {
	metadata := parseCodexTurnMetadata(headerValue(headers, "X-Codex-Turn-Metadata"))
	if metadata.AgentName != "" {
		info.AgentName = metadata.AgentName
	}
	if metadata.RequestKind != "" {
		info.RequestKind = metadata.RequestKind
	}
	if info.AgentID == "" && metadata.ThreadID != "" {
		info.AgentID = metadata.ThreadID
	}

	threadSource := strings.ToLower(metadata.ThreadSource)
	if threadSource != "" {
		if threadSource == "subagent" {
			if !*roleDecided {
				setAgentRole(info, "subagent", "header:x-codex-turn-metadata:thread_source")
				*roleDecided = true
			}
			setAgentTypeIfEmpty(info, metadata.SubagentKind)
		} else if !*roleDecided {
			// A non-empty thread_source is the sole authority for Codex role
			// detection, including main-thread requests that also carry a parent
			// thread header.
			setAgentRole(info, "main", "header:x-codex-turn-metadata:thread_source")
			*roleDecided = true
		}
		return
	}

	parentThreadID := headerValue(headers, "X-Codex-Parent-Thread-Id")
	if parentThreadID != "" && info.ParentSessionID == "" {
		info.ParentSessionID = parentThreadID
	}
	if *roleDecided {
		return
	}

	switch {
	case headerValue(headers, "X-Openai-Subagent") != "":
		setAgentRole(info, "subagent", "header:x-openai-subagent")
		*roleDecided = true
	case parentThreadID != "":
		setAgentRole(info, "subagent", "header:x-codex-parent-thread-id")
		*roleDecided = true
	}
}

type codexTurnMetadata struct {
	ThreadSource string `json:"thread_source"`
	RequestKind  string `json:"request_kind"`
	SubagentKind string `json:"subagent_kind"`
	AgentName    string `json:"agent_name"`
	ThreadID     string `json:"thread_id"`
}

func parseCodexTurnMetadata(raw string) codexTurnMetadata {
	var metadata codexTurnMetadata
	if raw == "" || json.Unmarshal([]byte(raw), &metadata) != nil {
		return codexTurnMetadata{}
	}
	metadata.ThreadSource = strings.TrimSpace(metadata.ThreadSource)
	metadata.RequestKind = strings.TrimSpace(metadata.RequestKind)
	metadata.SubagentKind = strings.TrimSpace(metadata.SubagentKind)
	metadata.AgentName = strings.TrimSpace(metadata.AgentName)
	metadata.ThreadID = strings.TrimSpace(metadata.ThreadID)
	return metadata
}

func setAgentRole(info *AgentInfo, role, source string) {
	info.AgentRole = role
	info.IsSubagent = role == "subagent"
	info.AgentSignalSource = source
}

func setAgentTypeIfEmpty(info *AgentInfo, agentType string) {
	if info.AgentType == "" && agentType != "" {
		info.AgentType = agentType
	}
}

func isKnownClientType(clientType string) bool {
	switch clientType {
	case clientTypeCSC, clientTypeCodex, clientTypeClaudeCode, clientTypeDSH,
		clientTypeZCode, clientTypeOpenCode, clientTypePi:
		return true
	default:
		return false
	}
}

func normalizeClientType(value string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "csc":
		return clientTypeCSC, true
	case "codex":
		return clientTypeCodex, true
	case "claude-code", "claude_code", "claude code":
		return clientTypeClaudeCode, true
	case "dsh", "dsh[acp]":
		return clientTypeDSH, true
	case "zcode":
		return clientTypeZCode, true
	case "opencode":
		return clientTypeOpenCode, true
	case "pi":
		return clientTypePi, true
	default:
		return "", false
	}
}

func clientTypeFromUserAgent(userAgent string) (string, bool) {
	ua := strings.ToLower(strings.TrimSpace(userAgent))
	switch {
	case strings.HasPrefix(ua, "zcode/"):
		return clientTypeZCode, true
	case strings.HasPrefix(ua, "csc/"):
		return clientTypeCSC, true
	case strings.HasPrefix(ua, "dsh/"), strings.HasPrefix(ua, "deepseek-harness/"):
		return clientTypeDSH, true
	case strings.HasPrefix(ua, "opencode/"):
		return clientTypeOpenCode, true
	case strings.HasPrefix(ua, "pi-cs-bridge"), strings.HasPrefix(ua, "pi/"):
		return clientTypePi, true
	case strings.HasPrefix(ua, "codex_cli_rs/"):
		return clientTypeCodex, true
	case strings.HasPrefix(ua, "claude-cli/"), strings.HasPrefix(ua, "claude-code/"):
		return clientTypeClaudeCode, true
	default:
		return "", false
	}
}

func csBridgeProductVersion(userAgent string) (string, bool) {
	for _, field := range strings.Fields(strings.TrimSpace(userAgent)) {
		token := strings.Trim(field, "()[]{};,")
		lower := strings.ToLower(token)
		if strings.HasPrefix(lower, "cs-bridge/") {
			return strings.TrimSpace(token[len("cs-bridge/"):]), true
		}
	}
	return "", false
}

func headerValue(headers http.Header, name string) string {
	for key, values := range headers {
		if !strings.EqualFold(key, name) {
			continue
		}
		for _, value := range values {
			if trimmed := strings.TrimSpace(value); trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}
