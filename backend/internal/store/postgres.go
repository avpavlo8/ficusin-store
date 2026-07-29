package store

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

func Open(ctx context.Context, cfg config.Database) (*pgxpool.Pool, error) {
	poolConfig, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}

	if !cfg.VerifyTLS {
		poolConfig.ConnConfig.TLSConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec
	} else if cfg.CA != "" {
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM([]byte(cfg.CA)) {
			return nil, errors.New("DATABASE_SSL_CA does not contain a valid certificate")
		}

		tlsConfig := poolConfig.ConnConfig.TLSConfig
		if tlsConfig == nil {
			tlsConfig = &tls.Config{MinVersion: tls.VersionTLS12}
		} else {
			tlsConfig = tlsConfig.Clone()
		}
		tlsConfig.RootCAs = roots
		poolConfig.ConnConfig.TLSConfig = tlsConfig
	}

	poolConfig.MaxConns = 10
	poolConfig.MinConns = 1
	poolConfig.MaxConnLifetime = time.Hour
	poolConfig.MaxConnIdleTime = 20 * time.Minute
	poolConfig.HealthCheckPeriod = time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("create PostgreSQL pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping PostgreSQL: %w", err)
	}

	return pool, nil
}
