package atproto

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/bluesky-social/indigo/atproto/atclient"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

var (
	// ErrSessionExpired は PDS の OAuth セッションが失効していて書き込めないことを表す（03 §2.2・§4）。
	// service はこれを 401 atproto_session_expired に写す。
	ErrSessionExpired = errors.New("atproto session expired")
	// ErrReauthRequired は必要な scope を持たないため PDS が書き込みを拒否したことを表す（17 §2.4）。
	// 認可サーバの実行値では、scope 不足は 403（ScopeMissingError）で返る。indigo は scope 固有の
	// 定数を持たないため、403 全般をこれに写す。service は 403 atproto_reauth_required に写す。
	ErrReauthRequired = errors.New("atproto reauth required")
	// ErrRecordNotFound は PDS に record が無いことを表す。削除では成功として扱う（17 §2.2）。
	ErrRecordNotFound = errors.New("atproto record not found")
	// ErrSwapConflict は putRecord の swapRecord が PDS の現在の cid と一致しないことを表す（13 §1.3）。
	// 読んでから書くまでの間に Bluesky のアプリなどで変更された場合に返り、service は 409 conflict に写す。
	ErrSwapConflict = errors.New("atproto swap conflict")
)

// Blob は com.atproto.repo.uploadBlob が返す blob の参照。record の avatar / banner にそのまま入れる（13 §1.3）。
type Blob struct {
	Type     string   `json:"$type"`
	Ref      BlobLink `json:"ref"`
	MimeType string   `json:"mimeType"`
	Size     int      `json:"size"`
}

// BlobLink は blob の CID を包む（{"$link": "..."} の形）。
type BlobLink struct {
	Link string `json:"$link"`
}

// Writer は com.atproto.repo の putRecord / createRecord / deleteRecord を、ユーザーの OAuth
// セッション（DPoP）で呼ぶ（03 §4）。トークンと DPoP 鍵は Store が持つ暗号化済みのセッションから
// 読み、ログには出さない。
type Writer struct {
	App *oauth.ClientApp
}

func NewWriter(app *oauth.ClientApp) *Writer { return &Writer{App: app} }

// PutRecord は record をユーザーの PDS に書く（rkey を指定する）。返り値は uri と cid。
// swapRecord を渡すと、PDS の現在の cid と一致しない場合に ErrSwapConflict を返す（13 §1.3）。
// swapRecord が空なら検査しない（新規作成）。セッションが失効していれば ErrSessionExpired、
// scope が足りなければ ErrReauthRequired、それ以外の PDS のエラーはそのまま返す。
func (w *Writer) PutRecord(ctx context.Context, did, atprotoSessionID, collection, rkey string, record any, swapRecord string) (string, string, error) {
	sess, err := w.resume(ctx, did, atprotoSessionID)
	if err != nil {
		return "", "", err
	}

	body := map[string]any{
		"repo":       did,
		"collection": collection,
		"rkey":       rkey,
		"record":     record,
	}
	if swapRecord != "" {
		body["swapRecord"] = swapRecord
	}
	var out recordRef
	if err := sess.APIClient().Post(ctx, syntax.NSID("com.atproto.repo.putRecord"), body, &out); err != nil {
		return "", "", classifyWriteError("putRecord", err)
	}
	return out.URI, out.CID, nil
}

// GetRecord は record をユーザーのセッションで読む。返り値は value・cid・存在するか。
// record が無い場合（未作成）は found=false・err=nil として扱う（13 §1.1、プロフィール未設定のユーザーがいるため）。
func (w *Writer) GetRecord(ctx context.Context, did, atprotoSessionID, collection, rkey string) (map[string]any, string, bool, error) {
	sess, err := w.resume(ctx, did, atprotoSessionID)
	if err != nil {
		return nil, "", false, err
	}

	params := map[string]any{"repo": did, "collection": collection, "rkey": rkey}
	var out struct {
		CID   string         `json:"cid"`
		Value map[string]any `json:"value"`
	}
	if err := sess.APIClient().Get(ctx, syntax.NSID("com.atproto.repo.getRecord"), params, &out); err != nil {
		classified := classifyWriteError("getRecord", err)
		if errors.Is(classified, ErrRecordNotFound) {
			return nil, "", false, nil
		}
		return nil, "", false, classified
	}
	return out.Value, out.CID, true, nil
}

// UploadBlob は画像などのバイト列を com.atproto.repo.uploadBlob で上げ、blob の参照を返す（13 §1.3）。
// 失敗の分類は PutRecord と同じ（失効・scope 不足・その他）。
func (w *Writer) UploadBlob(ctx context.Context, did, atprotoSessionID, mime string, data []byte) (*Blob, error) {
	sess, err := w.resume(ctx, did, atprotoSessionID)
	if err != nil {
		return nil, err
	}

	req := atclient.NewAPIRequest(http.MethodPost, syntax.NSID("com.atproto.repo.uploadBlob"), bytes.NewReader(data))
	req.Headers.Set("Content-Type", mime)
	resp, err := sess.APIClient().Do(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("uploadBlob: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var eb atclient.ErrorBody
		if derr := json.NewDecoder(resp.Body).Decode(&eb); derr != nil {
			return nil, classifyWriteError("uploadBlob", &atclient.APIError{StatusCode: resp.StatusCode})
		}
		return nil, classifyWriteError("uploadBlob", eb.APIError(resp.StatusCode))
	}
	var out struct {
		Blob Blob `json:"blob"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("uploadBlob: decode: %w", err)
	}
	return &out.Blob, nil
}

// CreateRecord は record をユーザーの PDS に新規に書く。rkey は PDS が TID で採番する（03 §6）。
// 返り値の uri から rkey（最後の path 要素）を取り出せる。エラーの分類は PutRecord と同じ。
func (w *Writer) CreateRecord(ctx context.Context, did, atprotoSessionID, collection string, record any) (string, string, error) {
	sess, err := w.resume(ctx, did, atprotoSessionID)
	if err != nil {
		return "", "", err
	}

	body := map[string]any{
		"repo":       did,
		"collection": collection,
		"record":     record,
	}
	var out recordRef
	if err := sess.APIClient().Post(ctx, syntax.NSID("com.atproto.repo.createRecord"), body, &out); err != nil {
		return "", "", classifyWriteError("createRecord", err)
	}
	return out.URI, out.CID, nil
}

// DeleteRecord はユーザーの PDS の record を消す。既に無い場合（ErrRecordNotFound）は
// 呼び出し側が成功として扱う（17 §2.2）。
func (w *Writer) DeleteRecord(ctx context.Context, did, atprotoSessionID, collection, rkey string) error {
	sess, err := w.resume(ctx, did, atprotoSessionID)
	if err != nil {
		return err
	}

	body := map[string]any{
		"repo":       did,
		"collection": collection,
		"rkey":       rkey,
	}
	if err := sess.APIClient().Post(ctx, syntax.NSID("com.atproto.repo.deleteRecord"), body, nil); err != nil {
		return classifyWriteError("deleteRecord", err)
	}
	return nil
}

type recordRef struct {
	URI string `json:"uri"`
	CID string `json:"cid"`
}

// resume は保存済みのセッションを読み直す。行が無ければ失効として扱う。
func (w *Writer) resume(ctx context.Context, did, atprotoSessionID string) (*oauth.ClientSession, error) {
	sess, err := w.App.ResumeSession(ctx, syntax.DID(did), atprotoSessionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrSessionExpired
		}
		return nil, fmt.Errorf("resume session: %w", err)
	}
	return sess, nil
}

// classifyWriteError は PDS の応答を、セッション失効（401）・再認可の必要（403）・
// record が無い（ErrRecordNotFound）・swapRecord の不一致（ErrSwapConflict）・
// それ以外の上流エラーに分ける（03 §2.2、13 §1.3、17 §2.4）。
func classifyWriteError(op string, err error) error {
	var apiErr *atclient.APIError
	if errors.As(err, &apiErr) {
		switch {
		case apiErr.StatusCode == 401:
			return ErrSessionExpired
		case apiErr.StatusCode == 403:
			// scope 不足（ScopeMissingError など）は 403 で返る。再ログインが要る（17 §2.4）。
			return ErrReauthRequired
		case apiErr.Name == "InvalidSwap":
			return ErrSwapConflict
		case apiErr.StatusCode == 404 || apiErr.Name == "RecordNotFound":
			return ErrRecordNotFound
		}
		return fmt.Errorf("%s: %w", op, err)
	}
	// アクセストークンの期限切れで refresh に失敗した場合、indigo はこの文言でエラーを返す。
	if strings.Contains(err.Error(), "refresh OAuth tokens") {
		return ErrSessionExpired
	}
	return fmt.Errorf("%s: %w", op, err)
}
