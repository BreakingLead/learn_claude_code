package agent

import (
	"strings"
	"testing"
)

func TestTeamSendAndConsumeInboxPersistsJSONL(t *testing.T) {
	rt := newAgentRuntime(testConfig(t.TempDir()), nil, nil)

	result := rt.runTeamSendMessage([]byte(`{"target":"alice","content":"hello"}`))
	if !strings.Contains(result, "Sent msg_") {
		t.Fatalf("unexpected send result: %s", result)
	}

	inbox := rt.runTeamCheckInbox([]byte(`{"agent":"alice"}`))
	if !strings.Contains(inbox, "<team_inbox>") || !strings.Contains(inbox, "hello") || !strings.Contains(inbox, "From: lead") {
		t.Fatalf("unexpected inbox: %s", inbox)
	}

	second := rt.runTeamCheckInbox([]byte(`{"agent":"alice"}`))
	if second != "(no team messages)" {
		t.Fatalf("expected cursor to consume inbox once, got %s", second)
	}
}

func TestTeamShutdownRequestAutoRespondsWhenTeammateConsumes(t *testing.T) {
	rt := newAgentRuntime(testConfig(t.TempDir()), nil, nil)

	result := rt.runTeamRequestShutdown([]byte(`{"target":"alice","reason":"done"}`))
	if !strings.Contains(result, "Requested shutdown") {
		t.Fatalf("unexpected request result: %s", result)
	}
	states := rt.team.protocolStates()
	if len(states) != 1 || states[0].Status != teamProtocolPending {
		t.Fatalf("expected pending shutdown state, got %+v", states)
	}

	_ = rt.runTeamCheckInbox([]byte(`{"agent":"alice"}`))
	states = rt.team.protocolStates()
	if states[0].Status != teamProtocolApproved {
		t.Fatalf("expected shutdown response to approve request, got %+v", states[0])
	}

	leadInbox := rt.runTeamCheckInbox([]byte(`{"agent":"lead"}`))
	if !strings.Contains(leadInbox, "shutdown_response") || !strings.Contains(leadInbox, states[0].RequestID) {
		t.Fatalf("expected lead inbox to include shutdown response, got %s", leadInbox)
	}
}

func TestTeamPlanApprovalResponseMatchesRequestType(t *testing.T) {
	rt := newAgentRuntime(testConfig(t.TempDir()), nil, nil)

	result := rt.runTeamRequestPlanApproval([]byte(`{"sender":"bob","target":"lead","plan":"rewrite auth"}`))
	if !strings.Contains(result, "Requested plan approval") {
		t.Fatalf("unexpected request result: %s", result)
	}
	state := rt.team.protocolStates()[0]

	if _, ok := rt.team.matchResponse(teamMessageTypeShutdownRes, state.RequestID, true); ok {
		t.Fatal("shutdown response must not match plan approval request")
	}
	state = rt.team.protocolStates()[0]
	if state.Status != teamProtocolPending {
		t.Fatalf("mismatched response should leave request pending, got %+v", state)
	}

	response := rt.runTeamRespondProtocol([]byte(`{"request_id":"` + state.RequestID + `","approve":false,"sender":"lead","content":"too risky"}`))
	if !strings.Contains(response, "status=rejected") {
		t.Fatalf("unexpected response result: %s", response)
	}
	state = rt.team.protocolStates()[0]
	if state.Status != teamProtocolRejected {
		t.Fatalf("expected rejected state, got %+v", state)
	}
}

func TestTeamModuleExposesToolsPromptAndSnapshot(t *testing.T) {
	rt := newAgentRuntime(testConfig(t.TempDir()), nil, nil)
	names := toolNames(rt.buildTools())
	for _, want := range []string{"team_send_message", "team_check_inbox", "team_request_shutdown", "team_request_plan_approval", "team_respond_protocol", "team_protocol_status"} {
		if !hasString(names, want) {
			t.Fatalf("missing team tool %q in %v", want, names)
		}
	}

	prompt := rt.getSystemPrompt(names)
	if !strings.Contains(prompt, "Team Protocols") || !strings.Contains(prompt, "request_id") {
		t.Fatalf("missing team prompt block: %s", prompt)
	}

	snapshots := rt.modules.runtimeSnapshots()
	if _, ok := snapshots["team"]; !ok {
		t.Fatalf("missing team runtime snapshot: %#v", snapshots)
	}
}

func TestDisabledTeamModuleRemovesToolsAndSnapshot(t *testing.T) {
	config := testConfig(t.TempDir())
	config.DisabledModules = map[string]bool{"team": true}
	rt := newAgentRuntime(config, nil, nil)

	if rt.team != nil {
		t.Fatal("disabled team module should not initialize registry")
	}
	if hasString(toolNames(rt.buildTools()), "team_send_message") {
		t.Fatalf("disabled team module should not expose tools")
	}
	if _, ok := rt.modules.runtimeSnapshots()["team"]; ok {
		t.Fatal("disabled team module should not expose snapshot")
	}
}
