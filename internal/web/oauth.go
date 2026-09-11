package web

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/auth"
	"github.com/janekbaraniewski/openusage/internal/core"
)

const oauthFlowLifetime = 15 * time.Minute

type oauthFlow struct {
	AccountID  string
	ProviderID string
	ExpiresAt  time.Time
	InProgress bool
	Claude     *auth.ClaudeAuthorization
	Codex      *auth.CodexDeviceAuthorization
	Credential *core.OAuthCredential
}

func (s *Server) handleOAuthStart(w http.ResponseWriter, r *http.Request, accountID string) {
	if !s.requireMutation(w, r) {
		return
	}
	var patch oauthStartPatch
	if err := decodeControlJSON(w, r, &patch); err != nil {
		writeControlDecodeError(w, err)
		return
	}
	providerID, spec, ok := s.providerSpec(patch.ProviderID)
	if !ok || (providerID != "claude_code" && providerID != "codex") ||
		!spec.Auth.SupportsAuth(core.ProviderAuthTypeOAuth) {
		writeJSONError(w, http.StatusBadRequest, "provider does not support browser authorization")
		return
	}
	if err := s.validateExistingAccountProvider(accountID, providerID); err != nil {
		if errors.Is(err, errAccountProviderMismatch) {
			writeJSONError(w, http.StatusBadRequest, "account provider mismatch")
		} else {
			writeJSONError(w, http.StatusInternalServerError, "configuration unavailable")
		}
		return
	}

	flowID, err := newRequestToken()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "authorization could not be started")
		return
	}
	flow := &oauthFlow{AccountID: accountID, ProviderID: providerID}
	response := OAuthStartResponse{FlowID: flowID, ProviderID: providerID}
	switch providerID {
	case "claude_code":
		authorization, startErr := s.oauthClient.StartClaudeAuthorization()
		if startErr != nil {
			writeJSONError(w, http.StatusInternalServerError, "Claude authorization could not be started")
			return
		}
		expiresAt := s.nowUTC().Add(oauthFlowLifetime)
		flow.Claude = &authorization
		flow.ExpiresAt = expiresAt
		response.AuthorizationURL = authorization.AuthorizationURL
		response.ExpiresAt = expiresAt.Format(time.RFC3339)
	case "codex":
		authorization, startErr := s.oauthClient.StartCodexDeviceAuthorization(r.Context())
		if startErr != nil {
			status := http.StatusBadGateway
			if errors.Is(startErr, auth.ErrDeviceAuthDisabled) {
				status = http.StatusBadRequest
			}
			writeJSONError(w, status, startErr.Error())
			return
		}
		flow.Codex = &authorization
		flow.ExpiresAt = authorization.ExpiresAt
		response.VerificationURL = authorization.VerificationURL
		response.UserCode = authorization.UserCode
		response.IntervalSeconds = int64(authorization.Interval / time.Second)
		response.ExpiresAt = authorization.ExpiresAt.UTC().Format(time.RFC3339)
	}

	s.oauthMu.Lock()
	s.removeExpiredOAuthFlowsLocked()
	for id, existing := range s.oauthFlows {
		if existing != nil && existing.AccountID == accountID {
			delete(s.oauthFlows, id)
		}
	}
	s.oauthFlows[flowID] = flow
	s.oauthMu.Unlock()
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleOAuthComplete(w http.ResponseWriter, r *http.Request, accountID string) {
	if !s.requireMutation(w, r) {
		return
	}
	var patch oauthCompletePatch
	if err := decodeControlJSON(w, r, &patch); err != nil {
		writeControlDecodeError(w, err)
		return
	}
	flowID := strings.TrimSpace(patch.FlowID)
	if flowID == "" {
		writeJSONError(w, http.StatusBadRequest, "authorization flow is required")
		return
	}
	flow, status := s.claimOAuthFlow(flowID, accountID)
	if status != http.StatusOK {
		if status == http.StatusConflict {
			writeJSON(w, status, map[string]any{"error": oauthFlowStatusMessage(status), "retryable": true})
		} else {
			writeJSONError(w, status, oauthFlowStatusMessage(status))
		}
		return
	}

	var credential core.OAuthCredential
	var err error
	if flow.Credential != nil {
		credential = *flow.Credential
	} else {
		switch flow.ProviderID {
		case "claude_code":
			if strings.TrimSpace(patch.AuthorizationResponse) == "" {
				s.finishOAuthFlow(flowID, false)
				writeJSONError(w, http.StatusBadRequest, "Claude authorization code is required")
				return
			}
			credential, err = s.oauthClient.ExchangeClaudeAuthorization(r.Context(), *flow.Claude, patch.AuthorizationResponse)
		case "codex":
			credential, err = s.oauthClient.CompleteCodexDeviceAuthorization(r.Context(), *flow.Codex)
		}
	}
	if errors.Is(err, auth.ErrAuthorizationPending) {
		s.finishOAuthFlow(flowID, true)
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "pending"})
		return
	}
	if err != nil {
		s.finishOAuthFlow(flowID, false)
		status := http.StatusBadGateway
		if errors.Is(err, auth.ErrAuthorizationExpired) || strings.Contains(err.Error(), "state mismatch") || strings.Contains(err.Error(), "does not contain a code") {
			status = http.StatusBadRequest
		}
		writeJSONError(w, status, err.Error())
		return
	}

	s.oauthMu.Lock()
	current, currentOK := s.oauthFlows[flowID]
	if !currentOK || current != flow {
		s.oauthMu.Unlock()
		writeJSONError(w, http.StatusGone, "authorization flow expired; start again")
		return
	}
	credentialCopy := credential
	flow.Credential = &credentialCopy
	s.controlMu.Lock()
	if err := s.persistAccountConfig(accountID, flow.ProviderID, string(core.ProviderAuthTypeOAuth), "", nil); err != nil {
		s.controlMu.Unlock()
		flow.InProgress = false
		s.oauthMu.Unlock()
		if errors.Is(err, errAccountProviderMismatch) {
			writeJSONError(w, http.StatusBadRequest, "account provider mismatch")
		} else {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "account save failed", "retryable": true})
		}
		return
	}
	if err := s.saveOAuthCredential(accountID, credential); err != nil {
		s.controlMu.Unlock()
		flow.InProgress = false
		s.oauthMu.Unlock()
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "credential save failed", "retryable": true})
		return
	}
	s.controlMu.Unlock()
	delete(s.oauthFlows, flowID)
	s.oauthMu.Unlock()
	expiresAt, expired := credentialExpiry(credential.ExpiresAt, s.nowUTC())
	writeJSON(w, http.StatusOK, CredentialResponse{
		AccountID:  accountID,
		ProviderID: flow.ProviderID,
		Credential: CredentialStatusDTO{
			Present:     true,
			Kind:        string(core.ProviderAuthTypeOAuth),
			Source:      "stored",
			ExpiresAt:   expiresAt,
			Expired:     expired,
			Refreshable: strings.TrimSpace(credential.RefreshToken) != "",
		},
	})
}

func (s *Server) claimOAuthFlow(flowID, accountID string) (*oauthFlow, int) {
	s.oauthMu.Lock()
	defer s.oauthMu.Unlock()
	s.removeExpiredOAuthFlowsLocked()
	flow, ok := s.oauthFlows[flowID]
	if !ok || flow.AccountID != accountID {
		return nil, http.StatusGone
	}
	if flow.InProgress {
		return nil, http.StatusConflict
	}
	flow.InProgress = true
	return flow, http.StatusOK
}

func (s *Server) finishOAuthFlow(flowID string, keep bool) {
	s.oauthMu.Lock()
	defer s.oauthMu.Unlock()
	if keep {
		if flow := s.oauthFlows[flowID]; flow != nil {
			flow.InProgress = false
		}
		return
	}
	delete(s.oauthFlows, flowID)
}

func (s *Server) removeExpiredOAuthFlowsLocked() {
	now := s.nowUTC()
	for id, flow := range s.oauthFlows {
		if flow == nil || !now.Before(flow.ExpiresAt) {
			delete(s.oauthFlows, id)
		}
	}
}

func (s *Server) invalidateOAuthFlows(accountID string) {
	s.oauthMu.Lock()
	defer s.oauthMu.Unlock()
	for id, flow := range s.oauthFlows {
		if flow != nil && flow.AccountID == accountID {
			delete(s.oauthFlows, id)
		}
	}
}

func oauthFlowStatusMessage(status int) string {
	if status == http.StatusConflict {
		return "authorization check already in progress"
	}
	return "authorization flow expired; start again"
}
