package agent

// 模块说明：
// team 模块提供多 agent 协作的最小结构化协议层。消息总线是 append-only JSONL
// 文件，便于跨进程/跨会话观察和恢复；协议状态保存在内存中，负责把 request 和
// response 通过 request_id 关联起来。
//
// 第一版聚焦 Lead 侧协调能力：发送消息、读取 inbox、发起 shutdown/plan approval
// 请求、匹配响应。后续可以在这个总线之上扩展长期 teammate worker 和执行门控。

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
)

const (
	teamLeadID                 = "lead"
	teamMessageTypeMessage     = "message"
	teamMessageTypeShutdownReq = "shutdown_request"
	teamMessageTypeShutdownRes = "shutdown_response"
	teamMessageTypePlanReq     = "plan_approval_request"
	teamMessageTypePlanRes     = "plan_approval_response"

	teamProtocolShutdown     = "shutdown"
	teamProtocolPlanApproval = "plan_approval"

	teamProtocolPending  = "pending"
	teamProtocolApproved = "approved"
	teamProtocolRejected = "rejected"
)

type teamModule struct {
	rt *agentRuntime
}

type teamBus struct {
	mu   sync.Mutex
	path string
}

type teamMessage struct {
	ID        string         `json:"id"`
	CreatedAt string         `json:"created_at"`
	Sender    string         `json:"sender"`
	Target    string         `json:"target"`
	Type      string         `json:"type"`
	Content   string         `json:"content"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

type protocolState struct {
	RequestID string    `json:"request_id"`
	Type      string    `json:"type"`
	Sender    string    `json:"sender"`
	Target    string    `json:"target"`
	Status    string    `json:"status"`
	Payload   string    `json:"payload"`
	CreatedAt time.Time `json:"created_at"`
}

type teamRegistry struct {
	mu       sync.Mutex
	bus      *teamBus
	requests map[string]protocolState
	cursors  map[string]int
	emit     func(format string, args ...any)
}

func (m *teamModule) ID() string {
	return "team"
}

func (m *teamModule) Init(ctx ModuleContext) error {
	return nil
}

func (m *teamModule) PromptBlocks(ctx context.Context, req PromptRequest) ([]PromptBlock, error) {
	if m.rt == nil || m.rt.team == nil {
		return nil, nil
	}
	content := "Team collaboration is available through a JSONL message bus. " +
		"Use team_send_message for ordinary messages, team_check_inbox to consume lead inbox, " +
		"team_request_shutdown for graceful teammate shutdown, and team_request_plan_approval for risky plans. " +
		"Protocol responses are matched by request_id and tracked as pending, approved, or rejected."
	return []PromptBlock{{
		Module:  m.ID(),
		Name:    "Team Protocols",
		Source:  m.rt.config.TeamMessagesPath,
		Content: content,
	}}, nil
}

func (m *teamModule) ToolDefinitions() []anthropic.ToolParam {
	return teamToolDefinitions()
}

func (m *teamModule) ToolHandlers() map[string]ToolHandler {
	if m.rt == nil {
		return map[string]ToolHandler{}
	}
	return m.rt.teamToolHandlers()
}

func (m *teamModule) RuntimeSnapshot() any {
	if m.rt == nil || m.rt.team == nil {
		return nil
	}
	return m.rt.team.snapshot()
}

func (m *teamModule) BeforeModel(ctx context.Context, req TurnRequest) []anthropic.MessageParam {
	if m.rt == nil || m.rt.team == nil || req.AgentID != "main" {
		return nil
	}
	msgs, err := m.rt.team.consumeInbox(teamLeadID, true)
	if err != nil || len(msgs) == 0 {
		return nil
	}
	return []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(formatTeamInbox(msgs)))}
}

func (m *teamModule) AfterToolUse(ctx context.Context, event ToolUseEvent) {}

func (m *teamModule) AfterToolRound(ctx context.Context, event ToolRoundEvent) {}

func newTeamRegistry(path string, emit func(format string, args ...any)) *teamRegistry {
	return &teamRegistry{
		bus:      &teamBus{path: path},
		requests: map[string]protocolState{},
		cursors:  map[string]int{},
		emit:     emit,
	}
}

func (rt *agentRuntime) teamToolHandlers() map[string]ToolHandler {
	return map[string]ToolHandler{
		"team_send_message":          rt.runTeamSendMessage,
		"team_check_inbox":           rt.runTeamCheckInbox,
		"team_request_shutdown":      rt.runTeamRequestShutdown,
		"team_request_plan_approval": rt.runTeamRequestPlanApproval,
		"team_respond_protocol":      rt.runTeamRespondProtocol,
		"team_protocol_status":       rt.runTeamProtocolStatus,
	}
}

func teamToolDefinitions() []anthropic.ToolParam {
	return []anthropic.ToolParam{
		{
			Name:        "team_send_message",
			Description: anthropic.String("Send a structured JSONL message to another agent inbox."),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: map[string]any{
					"target":  map[string]any{"type": "string"},
					"content": map[string]any{"type": "string"},
					"sender":  map[string]any{"type": "string", "description": "Defaults to lead."},
				},
				Required: []string{"target", "content"},
			},
		},
		{
			Name:        "team_check_inbox",
			Description: anthropic.String("Read and route pending team messages for an agent inbox."),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: map[string]any{
					"agent":          map[string]any{"type": "string", "description": "Defaults to lead."},
					"route_protocol": map[string]any{"type": "boolean", "description": "Defaults to true."},
				},
			},
		},
		{
			Name:        "team_request_shutdown",
			Description: anthropic.String("Ask a teammate to gracefully shut down; response is tracked by request_id."),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: map[string]any{
					"target": map[string]any{"type": "string"},
					"reason": map[string]any{"type": "string"},
				},
				Required: []string{"target"},
			},
		},
		{
			Name:        "team_request_plan_approval",
			Description: anthropic.String("Submit a high-risk plan for approval through the team protocol."),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: map[string]any{
					"target": map[string]any{"type": "string", "description": "Approver, defaults to lead."},
					"sender": map[string]any{"type": "string", "description": "Requester, defaults to lead."},
					"plan":   map[string]any{"type": "string"},
				},
				Required: []string{"plan"},
			},
		},
		{
			Name:        "team_respond_protocol",
			Description: anthropic.String("Respond to a shutdown or plan approval request by request_id."),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: map[string]any{
					"request_id": map[string]any{"type": "string"},
					"approve":    map[string]any{"type": "boolean"},
					"sender":     map[string]any{"type": "string"},
					"content":    map[string]any{"type": "string"},
				},
				Required: []string{"request_id", "approve"},
			},
		},
		{
			Name:        "team_protocol_status",
			Description: anthropic.String("List tracked team protocol requests and their pending/approved/rejected status."),
			InputSchema: anthropic.ToolInputSchemaParam{Properties: map[string]any{}},
		},
	}
}

func (rt *agentRuntime) runTeamSendMessage(raw json.RawMessage) string {
	if rt == nil || rt.team == nil {
		return "Error: team module is disabled"
	}
	var input struct {
		Sender  string `json:"sender"`
		Target  string `json:"target"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	msg, err := rt.team.send(cleanAgentID(input.Sender, teamLeadID), input.Target, teamMessageTypeMessage, input.Content, nil)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	return fmt.Sprintf("Sent %s to %s", msg.ID, msg.Target)
}

func (rt *agentRuntime) runTeamCheckInbox(raw json.RawMessage) string {
	if rt == nil || rt.team == nil {
		return "Error: team module is disabled"
	}
	var input struct {
		Agent         string `json:"agent"`
		RouteProtocol *bool  `json:"route_protocol"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	route := true
	if input.RouteProtocol != nil {
		route = *input.RouteProtocol
	}
	msgs, err := rt.team.consumeInbox(cleanAgentID(input.Agent, teamLeadID), route)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	if len(msgs) == 0 {
		return "(no team messages)"
	}
	return formatTeamInbox(msgs)
}

func (rt *agentRuntime) runTeamRequestShutdown(raw json.RawMessage) string {
	if rt == nil || rt.team == nil {
		return "Error: team module is disabled"
	}
	var input struct {
		Target string `json:"target"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	state, err := rt.team.request(teamProtocolShutdown, teamLeadID, input.Target, input.Reason, teamMessageTypeShutdownReq)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	return fmt.Sprintf("Requested shutdown from %s (%s)", state.Target, state.RequestID)
}

func (rt *agentRuntime) runTeamRequestPlanApproval(raw json.RawMessage) string {
	if rt == nil || rt.team == nil {
		return "Error: team module is disabled"
	}
	var input struct {
		Sender string `json:"sender"`
		Target string `json:"target"`
		Plan   string `json:"plan"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	state, err := rt.team.request(teamProtocolPlanApproval, cleanAgentID(input.Sender, teamLeadID), cleanAgentID(input.Target, teamLeadID), input.Plan, teamMessageTypePlanReq)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	return fmt.Sprintf("Requested plan approval from %s (%s)", state.Target, state.RequestID)
}

func (rt *agentRuntime) runTeamRespondProtocol(raw json.RawMessage) string {
	if rt == nil || rt.team == nil {
		return "Error: team module is disabled"
	}
	var input struct {
		RequestID string `json:"request_id"`
		Approve   bool   `json:"approve"`
		Sender    string `json:"sender"`
		Content   string `json:"content"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	state, msg, err := rt.team.respond(input.RequestID, cleanAgentID(input.Sender, ""), input.Approve, input.Content)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	return fmt.Sprintf("Sent %s for %s; status=%s", msg.Type, state.RequestID, state.Status)
}

func (rt *agentRuntime) runTeamProtocolStatus(raw json.RawMessage) string {
	if rt == nil || rt.team == nil {
		return "Error: team module is disabled"
	}
	states := rt.team.protocolStates()
	if len(states) == 0 {
		return "(no protocol requests)"
	}
	var lines []string
	for _, state := range states {
		lines = append(lines, fmt.Sprintf("- %s [%s] %s %s -> %s", state.RequestID, state.Status, state.Type, state.Sender, state.Target))
	}
	return strings.Join(lines, "\n")
}

func (r *teamRegistry) send(sender string, target string, msgType string, content string, metadata map[string]any) (teamMessage, error) {
	sender = cleanAgentID(sender, teamLeadID)
	target = cleanAgentID(target, "")
	msgType = strings.TrimSpace(msgType)
	content = strings.TrimSpace(content)
	if target == "" {
		return teamMessage{}, fmt.Errorf("target is required")
	}
	if msgType == "" {
		msgType = teamMessageTypeMessage
	}
	msg := teamMessage{
		ID:        newTeamID("msg"),
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Sender:    sender,
		Target:    target,
		Type:      msgType,
		Content:   content,
		Metadata:  metadata,
	}
	if err := r.bus.append(msg); err != nil {
		return teamMessage{}, err
	}
	if r.emit != nil {
		r.emit("[team] %s -> %s %s", sender, target, msgType)
	}
	return msg, nil
}

func (r *teamRegistry) request(protocolType string, sender string, target string, payload string, msgType string) (protocolState, error) {
	requestID := newTeamID("req")
	state := protocolState{
		RequestID: requestID,
		Type:      protocolType,
		Sender:    cleanAgentID(sender, teamLeadID),
		Target:    cleanAgentID(target, ""),
		Status:    teamProtocolPending,
		Payload:   strings.TrimSpace(payload),
		CreatedAt: time.Now().UTC(),
	}
	if state.Target == "" {
		return protocolState{}, fmt.Errorf("target is required")
	}
	r.mu.Lock()
	r.requests[requestID] = state
	r.mu.Unlock()
	_, err := r.send(state.Sender, state.Target, msgType, state.Payload, map[string]any{"request_id": requestID})
	if err != nil {
		r.mu.Lock()
		delete(r.requests, requestID)
		r.mu.Unlock()
		return protocolState{}, err
	}
	return state, nil
}

func (r *teamRegistry) respond(requestID string, sender string, approve bool, content string) (protocolState, teamMessage, error) {
	requestID = strings.TrimSpace(requestID)
	r.mu.Lock()
	state, ok := r.requests[requestID]
	r.mu.Unlock()
	if !ok {
		return protocolState{}, teamMessage{}, fmt.Errorf("unknown request_id: %s", requestID)
	}
	responseType, err := responseTypeForProtocol(state.Type)
	if err != nil {
		return protocolState{}, teamMessage{}, err
	}
	if sender == "" {
		sender = state.Target
	}
	if strings.TrimSpace(content) == "" {
		if approve {
			content = "Approved."
		} else {
			content = "Rejected."
		}
	}
	msg, err := r.send(sender, state.Sender, responseType, content, map[string]any{"request_id": requestID, "approve": approve})
	if err != nil {
		return protocolState{}, teamMessage{}, err
	}
	updated, ok := r.matchResponse(responseType, requestID, approve)
	if !ok {
		return protocolState{}, teamMessage{}, fmt.Errorf("response did not match pending request: %s", requestID)
	}
	return updated, msg, nil
}

func (r *teamRegistry) consumeInbox(agent string, routeProtocol bool) ([]teamMessage, error) {
	agent = cleanAgentID(agent, teamLeadID)
	all, err := r.bus.readAll()
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	start := r.cursors[agent]
	if start > len(all) {
		start = 0
	}
	r.cursors[agent] = len(all)
	r.mu.Unlock()
	var inbox []teamMessage
	for _, msg := range all[start:] {
		if msg.Target != agent {
			continue
		}
		if routeProtocol {
			r.dispatchMessage(agent, msg)
		}
		inbox = append(inbox, msg)
	}
	return inbox, nil
}

func (r *teamRegistry) dispatchMessage(agent string, msg teamMessage) bool {
	requestID, _ := msg.Metadata["request_id"].(string)
	if requestID == "" {
		return false
	}
	if strings.HasSuffix(msg.Type, "_response") {
		approve, _ := msg.Metadata["approve"].(bool)
		r.matchResponse(msg.Type, requestID, approve)
		return false
	}
	if msg.Type == teamMessageTypeShutdownReq && agent != teamLeadID {
		_, _, _ = r.respond(requestID, agent, true, "Shutting down.")
		return true
	}
	return false
}

func (r *teamRegistry) matchResponse(responseType string, requestID string, approve bool) (protocolState, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	state, ok := r.requests[requestID]
	if !ok || state.Status != teamProtocolPending || !responseMatchesProtocol(state.Type, responseType) {
		return protocolState{}, false
	}
	if approve {
		state.Status = teamProtocolApproved
	} else {
		state.Status = teamProtocolRejected
	}
	r.requests[requestID] = state
	return state, true
}

func (r *teamRegistry) protocolStates() []protocolState {
	r.mu.Lock()
	defer r.mu.Unlock()
	states := make([]protocolState, 0, len(r.requests))
	for _, state := range r.requests {
		states = append(states, state)
	}
	sort.Slice(states, func(i, j int) bool {
		return states[i].CreatedAt.Before(states[j].CreatedAt)
	})
	return states
}

func (r *teamRegistry) snapshot() any {
	states := r.protocolStates()
	statusCounts := map[string]int{}
	for _, state := range states {
		statusCounts[state.Status]++
	}
	return map[string]any{
		"messagesPath": r.bus.path,
		"requests":     states,
		"statusCounts": statusCounts,
	}
}

func (b *teamBus) append(msg teamMessage) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(b.path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(b.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	encoded, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	if _, err := file.Write(append(encoded, '\n')); err != nil {
		return err
	}
	return nil
}

func (b *teamBus) readAll() ([]teamMessage, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	file, err := os.Open(b.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer file.Close()
	var messages []teamMessage
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var msg teamMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}
		messages = append(messages, msg)
	}
	return messages, scanner.Err()
}

func responseTypeForProtocol(protocolType string) (string, error) {
	switch protocolType {
	case teamProtocolShutdown:
		return teamMessageTypeShutdownRes, nil
	case teamProtocolPlanApproval:
		return teamMessageTypePlanRes, nil
	default:
		return "", fmt.Errorf("unknown protocol type: %s", protocolType)
	}
}

func responseMatchesProtocol(protocolType string, responseType string) bool {
	expected, err := responseTypeForProtocol(protocolType)
	return err == nil && expected == responseType
}

func formatTeamInbox(messages []teamMessage) string {
	var lines []string
	lines = append(lines, "<team_inbox>")
	for _, msg := range messages {
		requestID := ""
		if msg.Metadata != nil {
			if value, ok := msg.Metadata["request_id"].(string); ok {
				requestID = " request_id=" + value
			}
		}
		lines = append(lines, fmt.Sprintf("## %s%s\nFrom: %s\nTo: %s\n%s", msg.Type, requestID, msg.Sender, msg.Target, msg.Content))
	}
	lines = append(lines, "</team_inbox>")
	return strings.Join(lines, "\n")
}

func cleanAgentID(id string, fallback string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	if id == "" {
		return fallback
	}
	return id
}

func newTeamID(prefix string) string {
	var suffix [3]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
	}
	return fmt.Sprintf("%s_%s", prefix, hex.EncodeToString(suffix[:]))
}
