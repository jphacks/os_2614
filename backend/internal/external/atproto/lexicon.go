package atproto

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/bluesky-social/indigo/atproto/lexicon"

	"github.com/applemango/chain/lexicons"
)

// lexicon JSON による record の検証（specification/backend/19-openness.md §6.1、03 §6）。
// PDS に書く前と Jetstream / listRecords の取り込みで、同じ検証器を通す。

// SelfLabelsType は record の labels に入れる union の $type（19 §2.15.3）。
const SelfLabelsType = "com.atproto.label.defs#selfLabels"

var (
	catalogOnce sync.Once
	catalog     *lexicon.BaseCatalog
	catalogErr  error
)

func loadCatalog() (*lexicon.BaseCatalog, error) {
	catalogOnce.Do(func() {
		c := lexicon.NewBaseCatalog()
		if err := c.LoadEmbedFS(lexicons.FS); err != nil {
			catalogErr = fmt.Errorf("load lexicons: %w", err)
			return
		}
		catalog = c
	})
	return catalog, catalogErr
}

// ValidateRecord は record（map など JSON にできる値）を collection の lexicon で検証する。
// record に $type が無ければ collection を補って検証する。本文はエラーに含めない（02 §7）が、
// indigo のエラーは項目名と上限だけを含む。
func ValidateRecord(collection string, record any) error {
	b, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("%s: %w", collection, err)
	}
	return ValidateRawRecord(collection, b)
}

// ValidateRawRecord は JSON の record を検証する。
func ValidateRawRecord(collection string, raw []byte) error {
	cat, err := loadCatalog()
	if err != nil {
		return err
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		return fmt.Errorf("%s: %w", collection, err)
	}
	if _, ok := data["$type"]; !ok {
		data["$type"] = collection
	}
	if err := lexicon.ValidateRecord(cat, data, collection, 0); err != nil {
		return fmt.Errorf("%s: %w", collection, err)
	}
	return nil
}

// SelfLabelsRecord は labels に書く selfLabels の値を作る。値が無ければ nil（labels を書かない）。
func SelfLabelsRecord(vals []string) map[string]any {
	if len(vals) == 0 {
		return nil
	}
	values := make([]map[string]any, 0, len(vals))
	for _, v := range vals {
		values = append(values, map[string]any{"val": v})
	}
	return map[string]any{"$type": SelfLabelsType, "values": values}
}

// selfLabelValues は labels（union）から val を取り出す。selfLabels 以外の $type は無視する。
func selfLabelValues(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var l struct {
		Type   string `json:"$type"`
		Values []struct {
			Val string `json:"val"`
		} `json:"values"`
	}
	if err := json.Unmarshal(raw, &l); err != nil || l.Type != SelfLabelsType {
		return nil
	}
	out := make([]string, 0, len(l.Values))
	seen := map[string]bool{}
	for _, v := range l.Values {
		if v.Val != "" && !seen[v.Val] {
			seen[v.Val] = true
			out = append(out, v.Val)
		}
	}
	return out
}
