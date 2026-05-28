package store

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/TorBox-App/torbox-fuse/internal/media"
	bolt "go.etcd.io/bbolt"
)

var filesBucket = []byte("files")

type Store struct{ db *bolt.DB }

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil && filepath.Dir(path) != "." {
		return nil, err
	}
	db, err := bolt.Open(path, 0600, nil)
	if err != nil {
		return nil, err
	}
	s := &Store{db: db}
	err = db.Update(func(tx *bolt.Tx) error { _, e := tx.CreateBucketIfNotExists(filesBucket); return e })
	if err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}
func (s *Store) ReplaceAll(ctx context.Context, records []media.FileRecord) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		_ = tx.DeleteBucket(filesBucket)
		b, err := tx.CreateBucket(filesBucket)
		if err != nil {
			return err
		}
		for _, r := range records {
			data, err := json.Marshal(r)
			if err != nil {
				return err
			}
			if err := b.Put([]byte(r.Key), data); err != nil {
				return err
			}
		}
		return nil
	})
}
func (s *Store) All(ctx context.Context) ([]media.FileRecord, error) {
	var out []media.FileRecord
	err := s.db.View(func(tx *bolt.Tx) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		b := tx.Bucket(filesBucket)
		return b.ForEach(func(k, v []byte) error {
			var r media.FileRecord
			if err := json.Unmarshal(v, &r); err != nil {
				return err
			}
			out = append(out, r)
			return nil
		})
	})
	return out, err
}
func (s *Store) Close() error { return s.db.Close() }
