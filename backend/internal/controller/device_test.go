//go:build integration

package controller_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/applemango/chain/internal/model"
	"github.com/applemango/chain/internal/testutil"
)

// 12 §2 の端末の登録・付け替え・削除。

func devicesOf(t *testing.T, env *testutil.Env, did string) []model.Device {
	t.Helper()
	devices, err := model.ListDevicesByDID(context.Background(), env.DB, did)
	if err != nil {
		t.Fatalf("ListDevicesByDID(%s): %v", did, err)
	}
	return devices
}

func TestDeviceRegisterAndReassign(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	env.CreateUser(t, "did:plc:bob", "bob.test")
	alice := env.Login(t, "did:plc:alice")
	bob := env.Login(t, "did:plc:bob")

	// 登録。
	status, body := testutil.Request(t, app, http.MethodPut, "/api/v1/me/devices",
		`{"fcmToken":"tok-shared","platform":"android"}`, alice)
	if status != http.StatusNoContent {
		t.Fatalf("register: status = %d, want 204 (body=%s)", status, body)
	}
	devices := devicesOf(t, env, "did:plc:alice")
	if len(devices) != 1 || devices[0].FCMToken != "tok-shared" || devices[0].Platform != "android" {
		t.Fatalf("alice devices = %+v, want tok-shared/android", devices)
	}

	// 同じトークンを bob が登録すると、alice から bob へ付け替わる（12 §2）。
	status, body = testutil.Request(t, app, http.MethodPut, "/api/v1/me/devices",
		`{"fcmToken":"tok-shared","platform":"ios"}`, bob)
	if status != http.StatusNoContent {
		t.Fatalf("reassign: status = %d, want 204 (body=%s)", status, body)
	}
	if devices := devicesOf(t, env, "did:plc:alice"); len(devices) != 0 {
		t.Fatalf("alice devices after reassign = %+v, want none", devices)
	}
	devices = devicesOf(t, env, "did:plc:bob")
	if len(devices) != 1 || devices[0].FCMToken != "tok-shared" || devices[0].Platform != "ios" {
		t.Fatalf("bob devices = %+v, want tok-shared/ios", devices)
	}

	// 同じユーザーが同じトークンを再登録しても行は増えず、platform が更新される。
	status, body = testutil.Request(t, app, http.MethodPut, "/api/v1/me/devices",
		`{"fcmToken":"tok-shared","platform":"android"}`, bob)
	if status != http.StatusNoContent {
		t.Fatalf("re-register: status = %d, want 204 (body=%s)", status, body)
	}
	devices = devicesOf(t, env, "did:plc:bob")
	if len(devices) != 1 || devices[0].Platform != "android" {
		t.Fatalf("bob devices after re-register = %+v, want one android row", devices)
	}
}

func TestDeviceDelete(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")
	env.CreateUser(t, "did:plc:bob", "bob.test")
	alice := env.Login(t, "did:plc:alice")
	bob := env.Login(t, "did:plc:bob")

	for _, token := range []string{"tok-a", "tok-b"} {
		status, body := testutil.Request(t, app, http.MethodPut, "/api/v1/me/devices",
			`{"fcmToken":"`+token+`","platform":"android"}`, alice)
		if status != http.StatusNoContent {
			t.Fatalf("register %s: status = %d, want 204 (body=%s)", token, status, body)
		}
	}

	// 他人のトークンは消せない。成功として返すが行は残る。
	status, body := testutil.Request(t, app, http.MethodDelete, "/api/v1/me/devices/tok-a", "", bob)
	if status != http.StatusNoContent {
		t.Fatalf("bob delete alice token: status = %d, want 204 (body=%s)", status, body)
	}
	if devices := devicesOf(t, env, "did:plc:alice"); len(devices) != 2 {
		t.Fatalf("alice devices after bob delete = %+v, want 2", devices)
	}

	// 本人は消せる。
	status, body = testutil.Request(t, app, http.MethodDelete, "/api/v1/me/devices/tok-a", "", alice)
	if status != http.StatusNoContent {
		t.Fatalf("alice delete: status = %d, want 204 (body=%s)", status, body)
	}
	devices := devicesOf(t, env, "did:plc:alice")
	if len(devices) != 1 || devices[0].FCMToken != "tok-b" {
		t.Fatalf("alice devices = %+v, want only tok-b", devices)
	}

	// 無いトークンでも成功（ログアウトを冪等にする）。
	status, body = testutil.Request(t, app, http.MethodDelete, "/api/v1/me/devices/tok-a", "", alice)
	if status != http.StatusNoContent {
		t.Fatalf("delete missing: status = %d, want 204 (body=%s)", status, body)
	}
}

func TestDeviceRegisterValidation(t *testing.T) {
	env := testutil.NewEnv(t)
	app := testutil.NewApp(env)
	env.CreateUser(t, "did:plc:alice", "alice.test")

	status, body := testutil.Request(t, app, http.MethodPut, "/api/v1/me/devices",
		`{"fcmToken":"tok","platform":"android"}`, "")
	if status != http.StatusUnauthorized {
		t.Fatalf("without token: status = %d, want 401 (body=%s)", status, body)
	}
	testutil.WantErrorCode(t, body, "unauthenticated")

	alice := env.Login(t, "did:plc:alice")

	status, body = testutil.Request(t, app, http.MethodPut, "/api/v1/me/devices",
		`{"fcmToken":"","platform":"android"}`, alice)
	if status != http.StatusBadRequest {
		t.Fatalf("empty fcmToken: status = %d, want 400 (body=%s)", status, body)
	}
	if e := testutil.WantErrorCode(t, body, "invalid_request"); e.Error.Details.Fields["fcmToken"] == "" {
		t.Fatalf("fields.fcmToken missing (body=%s)", body)
	}

	status, body = testutil.Request(t, app, http.MethodPut, "/api/v1/me/devices",
		`{"fcmToken":"tok","platform":"web"}`, alice)
	if status != http.StatusBadRequest {
		t.Fatalf("bad platform: status = %d, want 400 (body=%s)", status, body)
	}
	if e := testutil.WantErrorCode(t, body, "invalid_request"); e.Error.Details.Fields["platform"] == "" {
		t.Fatalf("fields.platform missing (body=%s)", body)
	}

	// platform 省略時は android とする（12 §2 は ios / android の 2 つ）。
	status, body = testutil.Request(t, app, http.MethodPut, "/api/v1/me/devices",
		`{"fcmToken":"tok-default"}`, alice)
	if status != http.StatusNoContent {
		t.Fatalf("default platform: status = %d, want 204 (body=%s)", status, body)
	}
	devices := devicesOf(t, env, "did:plc:alice")
	if len(devices) != 1 || devices[0].Platform != "android" {
		t.Fatalf("devices = %+v, want one android row", devices)
	}
}
