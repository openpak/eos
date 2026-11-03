package main

// Durable PUID -> BAAS user id (and Epic account id -> BAAS user id) in Postgres (EOS_DATABASE_URL),
// written at login and read back on a cache miss, so a restart forgets nobody. Memory is only a cache
// in front. The pool is capped at two connections. If the database is down the maps keep serving from
// memory, the failure is logged at ERROR level, and the write is retried at the next login of that
// player; at start an unreachable database is retried every 30 seconds.

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"openpak/eos/redact"
)

const (
	kindPUID = "puid"
	kindEpic = "epic"
)

type ownerStore struct{ pool *pgxpool.Pool }

const ownerSchema = `CREATE TABLE IF NOT EXISTS eos_owners (
	kind         text NOT NULL,
	id           text NOT NULL,
	baas_user_id text NOT NULL,
	last_seen    timestamptz NOT NULL DEFAULT now(),
	PRIMARY KEY (kind, id)
)`

func openOwnerStore(ctx context.Context, url string) (*ownerStore, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	cfg.MaxConns = 2
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if _, err := pool.Exec(ctx, ownerSchema); err != nil {
		pool.Close()
		return nil, err
	}
	return &ownerStore{pool}, nil
}

func (o *ownerStore) put(ctx context.Context, kind, id, baas string) error {
	_, err := o.pool.Exec(ctx, `INSERT INTO eos_owners (kind, id, baas_user_id, last_seen) VALUES ($1,$2,$3,now())
		ON CONFLICT (kind, id) DO UPDATE SET baas_user_id = EXCLUDED.baas_user_id, last_seen = now()`, kind, id, baas)
	return err
}

func (o *ownerStore) get(ctx context.Context, kind, id string) (string, bool, error) {
	var baas string
	err := o.pool.QueryRow(ctx, `SELECT baas_user_id FROM eos_owners WHERE kind = $1 AND id = $2`, kind, id).Scan(&baas)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	return baas, err == nil, err
}

func (s *server) cache(kind string) interface {
	Load(any) (any, bool)
	Store(any, any)
} {
	if kind == kindEpic {
		return &s.epicOwners
	}
	return &s.owners
}

// remember records key -> baas in the cache and, once, in Postgres.
func (s *server) remember(kind, key, baas string) {
	s.cache(kind).Store(key, baas)
	db := s.db.Load()
	if db == nil {
		return
	}
	pk := kind + "\x00" + key + "\x00" + baas
	if _, done := s.persisted.Load(pk); done {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := db.put(ctx, kind, key, baas); err != nil {
		log.Printf("[eos] ERROR saving the %s owner map failed (retried at the next login): %s", kind, redact.Err(err))
		return
	}
	s.persisted.Store(pk, true)
}

// owner answers from the cache, then from Postgres.
func (s *server) owner(kind, key string) (string, bool) {
	if v, ok := s.cache(kind).Load(key); ok {
		return v.(string), true
	}
	db := s.db.Load()
	if db == nil {
		return "", false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	v, ok, err := db.get(ctx, kind, key)
	if err != nil {
		log.Printf("[eos] ERROR reading the %s owner map failed: %s", kind, redact.Err(err))
		return "", false
	}
	if ok {
		s.cache(kind).Store(key, v)
		s.persisted.Store(kind+"\x00"+key+"\x00"+v, true)
	}
	return v, ok
}

// startStore connects to Postgres when url is set; when it is down at start it logs at ERROR level
// and keeps trying in the background.
func (s *server) startStore(ctx context.Context, url string) {
	if url == "" {
		log.Printf("[eos] EOS_DATABASE_URL is empty: the PUID owner map is memory only")
		return
	}
	try := func() error {
		cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		db, err := openOwnerStore(cctx, url)
		if err != nil {
			return err
		}
		if err := s.acct.attach(cctx, db.pool); err != nil {
			db.pool.Close()
			return err
		}
		s.db.Store(db)
		return nil
	}
	if err := try(); err == nil {
		log.Printf("[eos] owner map persisted in Postgres")
		return
	} else {
		log.Printf("[eos] ERROR Postgres unavailable, running on memory and retrying every 30s: %s", redact.Err(err))
	}
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if try() == nil {
					log.Printf("[eos] Postgres is back")
					return
				}
			}
		}
	}()
}
