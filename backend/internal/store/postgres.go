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

func configureTLS(poolConfig *pgxpool.Config, cfg config.Database) error {
	// DATABASE_SSL_VERIFY controls certificate verification, not whether the
	// connection uses TLS at all. pgx has already translated sslmode from the
	// DATABASE_URL: a nil TLSConfig means sslmode=disable and must stay nil.
	if !cfg.VerifyTLS {
		if poolConfig.ConnConfig.TLSConfig != nil {
			tlsConfig := poolConfig.ConnConfig.TLSConfig.Clone()
			tlsConfig.InsecureSkipVerify = true //nolint:gosec -- explicitly requested by DATABASE_SSL_VERIFY=false
			poolConfig.ConnConfig.TLSConfig = tlsConfig
		}
		return nil
	}

	if cfg.CA == "" || poolConfig.ConnConfig.TLSConfig == nil {
		return nil
	}

	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM([]byte(cfg.CA)) {
		return errors.New("DATABASE_SSL_CA does not contain a valid certificate")
	}

	tlsConfig := poolConfig.ConnConfig.TLSConfig.Clone()
	if tlsConfig.MinVersion == 0 {
		tlsConfig.MinVersion = tls.VersionTLS12
	}
	tlsConfig.RootCAs = roots
	poolConfig.ConnConfig.TLSConfig = tlsConfig
	return nil
}

func Open(ctx context.Context, cfg config.Database) (*pgxpool.Pool, error) {
	poolConfig, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	if err := configureTLS(poolConfig, cfg); err != nil {
		return nil, err
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
