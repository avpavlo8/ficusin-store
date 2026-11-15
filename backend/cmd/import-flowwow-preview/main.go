// Import a credential-free production link observation into isolated staging.
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/flowwow"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	connection := flag.String("connection", "", "private staging DB configuration JSON")
	snapshot := flag.String("snapshot", "", "credential-free production observation JSON")
	expected := flag.String("expected-database", "", "exact isolated database name")
	apply := flag.Bool("apply", false, "store the read-only observation; default is dry-run")
	flag.Parse()
	var c struct {
		URL         string `json:"url"`
		CA          string `json:"ca"`
		Environment string `json:"environment"`
		Mode        string `json:"mode"`
		ServerName  string `json:"serverName"`
	}
	raw, e := os.ReadFile(*connection)
	if e != nil || json.Unmarshal(raw, &c) != nil {
		fail("connection_config_unavailable")
	}
	if c.Environment != "staging" || c.Mode != "mock" {
		fail("isolated_staging_required")
	}
	u, e := url.Parse(c.URL)
	if e != nil {
		fail("invalid_database_config")
	}
	q := u.Query()
	q.Set("sslmode", "verify-full")
	u.RawQuery = q.Encode()
	file, e := os.Open(*snapshot)
	if e != nil {
		fail("snapshot_unavailable")
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 2<<20))
	decoder.DisallowUnknownFields()
	var input flowwow.LinkSnapshot
	if decoder.Decode(&input) != nil {
		fail("invalid_snapshot")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		fail("invalid_snapshot")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	poolConfig, e := pgxpool.ParseConfig(u.String())
	if e != nil || poolConfig.ConnConfig.TLSConfig == nil {
		fail("verified_tls_required")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM([]byte(strings.ReplaceAll(c.CA, "\\n", "\n"))) {
		fail("trusted_ca_required")
	}
	poolConfig.ConnConfig.TLSConfig.RootCAs = roots
	poolConfig.ConnConfig.TLSConfig.InsecureSkipVerify = false
	poolConfig.ConnConfig.TLSConfig.MinVersion = tls.VersionTLS12
	if c.ServerName != "" {
		poolConfig.ConnConfig.TLSConfig.ServerName = c.ServerName
	}
	poolConfig.MaxConns = 1
	pool, e := pgxpool.NewWithConfig(ctx, poolConfig)
	if e != nil {
		fail("database_connection_failed")
	}
	defer pool.Close()
	if pool.Ping(ctx) != nil {
		fail("verified_database_connection_failed")
	}
	if e = flowwow.ImportLinkPreview(ctx, pool, c.Environment, c.Mode, *expected, input, *apply); e != nil {
		fail("preview_import_failed: " + e.Error())
	}
	fmt.Printf("links=%d shop=%s applied=%t provider_writes=false\n", len(input.Items), input.ShopID, *apply)
}
func fail(message string) { fmt.Fprintln(os.Stderr, message); os.Exit(1) }
