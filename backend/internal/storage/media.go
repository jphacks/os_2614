package storage

import (
	"errors"
	"os"
	"path/filepath"
	"time"
)

// Variant は配信する画像の種類。
type Variant string

const (
	VariantFull  Variant = "full"
	VariantThumb Variant = "thumb"
)

// Store は data/media 配下のファイルを扱う。保存は {yyyy}/{mm}/{id}.jpg と {id}_t.jpg（10 §4.4）。
type Store struct {
	Dir string
}

func NewStore(dir string) *Store { return &Store{Dir: dir} }

func (s *Store) dir(createdAt time.Time) string {
	return filepath.Join(s.Dir, createdAt.UTC().Format("2006"), createdAt.UTC().Format("01"))
}

// Path は配信対象のファイルの場所を返す。
func (s *Store) Path(id string, createdAt time.Time, v Variant) string {
	name := id + ".jpg"
	if v == VariantThumb {
		name = id + "_t.jpg"
	}
	return filepath.Join(s.dir(createdAt), name)
}

// Save は再エンコード済みの本体とサムネイルを書く。
func (s *Store) Save(id string, createdAt time.Time, full, thumb []byte) error {
	if err := os.MkdirAll(s.dir(createdAt), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(s.Path(id, createdAt, VariantFull), full, 0o644); err != nil {
		return err
	}
	return os.WriteFile(s.Path(id, createdAt, VariantThumb), thumb, 0o644)
}

// Size はファイルの大きさを返す。なければ err。
func (s *Store) Size(id string, createdAt time.Time, v Variant) (int, error) {
	fi, err := os.Stat(s.Path(id, createdAt, v))
	if err != nil {
		return 0, err
	}
	return int(fi.Size()), nil
}

// Delete は本体とサムネイルを消す。既になければ何もしない。
func (s *Store) Delete(id string, createdAt time.Time) error {
	for _, v := range []Variant{VariantFull, VariantThumb} {
		if err := os.Remove(s.Path(id, createdAt, v)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}
