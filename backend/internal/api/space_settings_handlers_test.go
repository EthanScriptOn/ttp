package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/yuebuy/cicd-platform/backend/internal/domain"
	"github.com/yuebuy/cicd-platform/backend/internal/store"
)

func TestSpaceSettingsMemberLifecycleProtectsSensitiveAndOwnerData(t *testing.T) {
	server := testServer()
	handler := server.Router()
	adminToken := loginForTest(t, handler)

	settings := doRequest(t, handler, http.MethodGet, "/api/space/settings", adminToken, "")
	if settings.Code != http.StatusOK || !strings.Contains(settings.Body.String(), "实验室空间") {
		t.Fatalf("get space settings: %d %s", settings.Code, settings.Body.String())
	}

	updatedSettings := doRequest(t, handler, http.MethodPatch, "/api/space/settings", adminToken, `{"name":"研发空间","description":"用于发布验证"}`)
	if updatedSettings.Code != http.StatusOK || !strings.Contains(updatedSettings.Body.String(), "研发空间") || !strings.Contains(updatedSettings.Body.String(), `"role":"admin"`) {
		t.Fatalf("update space settings: %d %s", updatedSettings.Code, updatedSettings.Body.String())
	}

	created := doRequest(t, handler, http.MethodPost, "/api/space/members", adminToken, `{"username":"alice","display_name":"Alice","password":"alice-pass-123","role":"viewer"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create member: %d %s", created.Code, created.Body.String())
	}
	if strings.Contains(created.Body.String(), "alice-pass-123") || strings.Contains(created.Body.String(), "password_hash") {
		t.Fatalf("member response leaked credential data: %s", created.Body.String())
	}

	var createBody struct {
		Member domain.SpaceMember `json:"member"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &createBody); err != nil {
		t.Fatal(err)
	}
	if createBody.Member.UserID == 0 || createBody.Member.Role != "viewer" {
		t.Fatalf("unexpected created member: %#v", createBody.Member)
	}

	duplicate := doRequest(t, handler, http.MethodPost, "/api/space/members", adminToken, `{"username":"alice","password":"another-pass-123","role":"developer"}`)
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate member: expected 409, got %d: %s", duplicate.Code, duplicate.Body.String())
	}

	changed := doRequest(t, handler, http.MethodPatch, "/api/space/members/"+strconv.FormatUint(createBody.Member.UserID, 10), adminToken, `{"role":"developer"}`)
	if changed.Code != http.StatusOK || !strings.Contains(changed.Body.String(), `"role":"developer"`) {
		t.Fatalf("change member role: %d %s", changed.Code, changed.Body.String())
	}

	ownerChange := doRequest(t, handler, http.MethodPatch, "/api/space/members/1", adminToken, `{"role":"viewer"}`)
	if ownerChange.Code != http.StatusForbidden {
		t.Fatalf("change owner: expected 403, got %d: %s", ownerChange.Code, ownerChange.Body.String())
	}
	selfRemove := doRequest(t, handler, http.MethodDelete, "/api/space/members/1", adminToken, "")
	if selfRemove.Code != http.StatusForbidden {
		t.Fatalf("remove self: expected 403, got %d: %s", selfRemove.Code, selfRemove.Body.String())
	}

	removed := doRequest(t, handler, http.MethodDelete, "/api/space/members/"+strconv.FormatUint(createBody.Member.UserID, 10), adminToken, "")
	if removed.Code != http.StatusOK || !strings.Contains(removed.Body.String(), `"removed":true`) {
		t.Fatalf("remove member: %d %s", removed.Code, removed.Body.String())
	}
}

func TestSpaceSettingsReadOnlyMemberCannotMutateSpace(t *testing.T) {
	server := testServer()
	handler := server.Router()
	adminToken := loginForTest(t, handler)

	created := doRequest(t, handler, http.MethodPost, "/api/space/members", adminToken, `{"username":"readonly","display_name":"只读用户","password":"readonly-pass-123","role":"viewer"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create readonly member: %d %s", created.Code, created.Body.String())
	}
	var createBody struct {
		Member domain.SpaceMember `json:"member"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &createBody); err != nil {
		t.Fatal(err)
	}

	data, ok := server.deps.Store.(*store.Memory)
	if !ok {
		t.Fatal("test server is not using memory store")
	}
	user, err := data.User(t.Context(), createBody.Member.UserID)
	if err != nil {
		t.Fatalf("load created user: %v", err)
	}
	viewerToken, _, err := server.deps.Auth.Issue(user, "space-lab", "viewer")
	if err != nil {
		t.Fatalf("issue viewer token: %v", err)
	}

	members := doRequest(t, handler, http.MethodGet, "/api/space/members", viewerToken, "")
	if members.Code != http.StatusOK {
		t.Fatalf("viewer list members: %d %s", members.Code, members.Body.String())
	}
	updateSpace := doRequest(t, handler, http.MethodPatch, "/api/space/settings", viewerToken, `{"name":"不应成功"}`)
	if updateSpace.Code != http.StatusForbidden {
		t.Fatalf("viewer update space: expected 403, got %d: %s", updateSpace.Code, updateSpace.Body.String())
	}
	addMember := doRequest(t, handler, http.MethodPost, "/api/space/members", viewerToken, `{"username":"blocked","password":"blocked-pass-123","role":"viewer"}`)
	if addMember.Code != http.StatusForbidden {
		t.Fatalf("viewer add member: expected 403, got %d: %s", addMember.Code, addMember.Body.String())
	}
	changeRole := doRequest(t, handler, http.MethodPatch, "/api/space/members/"+strconv.FormatUint(createBody.Member.UserID, 10), viewerToken, `{"role":"developer"}`)
	if changeRole.Code != http.StatusForbidden {
		t.Fatalf("viewer change role: expected 403, got %d: %s", changeRole.Code, changeRole.Body.String())
	}
}
