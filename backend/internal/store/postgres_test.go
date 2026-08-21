package store

import (
	"testing"

	"github.com/avpavlo8/ficusin-store/backend/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestConfigureTLSPreservesSSLModeDisable(t *testing.T) {
	poolConfig, err := pgxpool.ParseConfig("postgres://postgres:postgres@127.0.0.1:5432/test?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	if poolConfig.ConnConfig.TLSConfig != nil {
		t.Fatal("pgx should disable TLS for sslmode=disable")
	}

	if err := configureTLS(poolConfig, config.Database{VerifyTLS: false}); err != nil {
		t.Fatal(err)
	}
	if poolConfig.ConnConfig.TLSConfig != nil {
		t.Fatal("DATABASE_SSL_VERIFY=false must not enable TLS when sslmode=disable")
	}
}

func TestConfigureTLSDisablesVerificationWithoutDisablingTLS(t *testing.T) {
	poolConfig, err := pgxpool.ParseConfig("postgres://postgres:postgres@localhost:5432/test?sslmode=require")
	if err != nil {
		t.Fatal(err)
	}
	if poolConfig.ConnConfig.TLSConfig == nil {
		t.Fatal("pgx should configure TLS for sslmode=require")
	}

	if err := configureTLS(poolConfig, config.Database{VerifyTLS: false}); err != nil {
		t.Fatal(err)
	}
	if poolConfig.ConnConfig.TLSConfig == nil || !poolConfig.ConnConfig.TLSConfig.InsecureSkipVerify {
		t.Fatal("DATABASE_SSL_VERIFY=false should keep TLS but disable certificate verification")
	}
}
