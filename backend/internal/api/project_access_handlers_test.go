package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/yuebuy/cicd-platform/backend/internal/domain"
)

func TestProjectAccessEndpointsFilterProjectsAndBindRoles(t *testing.T) {
	server := testServer()
	handler := server.Router()
	adminToken := loginForTest(t, handler)

	created := doRequest(t, handler, http.MethodPost, "/api/space/members", adminToken, `{"username":"project-user","display_name":"项目用户","password":"project-user-pass-123","role":"viewer"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create space member: %d %s", created.Code, created.Body.String())
	}
	var memberBody struct {
		Member domain.SpaceMember `json:"member"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &memberBody); err != nil {
		t.Fatal(err)
	}

	viewerToken, _, err := server.deps.Auth.Issue(domain.User{ID: memberBody.Member.UserID, Username: memberBody.Member.Username}, "space-lab", "viewer")
	if err != nil {
		t.Fatalf("issue viewer token: %v", err)
	}
	before := doRequest(t, handler, http.MethodGet, "/api/projects", viewerToken, "")
	if before.Code != http.StatusOK || before.Body.String() == "" {
		t.Fatalf("list projects before binding: %d %s", before.Code, before.Body.String())
	}
	if jsonCountItems(t, before.Body.Bytes()) != 0 {
		t.Fatal("viewer should not see projects before project membership")
	}

	bound := doRequest(t, handler, http.MethodPost, "/api/projects/reverse-lab/members", adminToken, `{"user_id":`+strconv.FormatUint(memberBody.Member.UserID, 10)+`,"role_key":"project_developer"}`)
	if bound.Code != http.StatusCreated {
		t.Fatalf("bind project member: %d %s", bound.Code, bound.Body.String())
	}
	after := doRequest(t, handler, http.MethodGet, "/api/projects", viewerToken, "")
	if after.Code != http.StatusOK || jsonCountItems(t, after.Body.Bytes()) != 1 {
		t.Fatalf("list projects after binding: %d %s", after.Code, after.Body.String())
	}
	access := doRequest(t, handler, http.MethodGet, "/api/projects/reverse-lab/access", viewerToken, "")
	if access.Code != http.StatusOK || !containsJSON(access.Body.String(), `"role_key":"project_developer"`) {
		t.Fatalf("get project access: %d %s", access.Code, access.Body.String())
	}
	roles := doRequest(t, handler, http.MethodGet, "/api/space/project-roles", viewerToken, "")
	if roles.Code != http.StatusOK || !containsJSON(roles.Body.String(), `"project_maintainer"`) {
		t.Fatalf("list project roles: %d %s", roles.Code, roles.Body.String())
	}
}

func jsonCountItems(t *testing.T, payload []byte) int {
	t.Helper()
	var body struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		t.Fatal(err)
	}
	return len(body.Items)
}

func containsJSON(value, needle string) bool {
	return len(value) >= len(needle) && strings.Contains(value, needle)
}
