package model

import (
	"context"

	"github.com/applemango/chain/internal/db"
)

// collectionInstancePin は pin の record の collection（10 §3）。author_did は Space の authority（04 §2.4）。
const collectionInstancePin = "jp.i32.chain.instance.pin"

// ListActivePins は Space 内の削除されていない pin を、作成が古い順で返す（10 §6）。
// 件数は最大 10 件程度なので、subject の一致は呼び出し側で value_json を見て判定する。
func ListActivePins(ctx context.Context, q db.Querier, spaceID string) ([]Record, error) {
	return db.GetItems(ctx, q,
		"SELECT "+recordColumns+" FROM records WHERE space_id = ? AND collection = ? AND deleted_at IS NULL ORDER BY created_at, uri",
		[]any{spaceID, collectionInstancePin}, scanRecord)
}

// GetActivePin は space_id と rkey で、削除されていない pin を返す。
func GetActivePin(ctx context.Context, q db.Querier, spaceID, rkey string) (*Record, error) {
	return db.GetItem(ctx, q,
		"SELECT "+recordColumns+" FROM records WHERE space_id = ? AND collection = ? AND rkey = ? AND deleted_at IS NULL",
		[]any{spaceID, collectionInstancePin, rkey}, scanRecord)
}
