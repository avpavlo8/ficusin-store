package catalogsync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// StagingProvider exercises the durable export workflow without credentials,
// HTTP requests or changes in the real Saby account. Its remote state lives
// exclusively in the isolated database, under a reserved ID namespace.
type StagingProvider struct{ pool *pgxpool.Pool }

func NewStagingProvider(ctx context.Context, pool *pgxpool.Pool) (*StagingProvider, error) {
	var database string
	if err := pool.QueryRow(ctx, "SELECT current_database()").Scan(&database); err != nil {
		return nil, err
	}
	if database != "ficusin_staging" && !strings.HasPrefix(database, "ficusin_staging_") {
		return nil, errors.New("Saby simulation requires an isolated staging database")
	}
	return &StagingProvider{pool: pool}, nil
}
func (p *StagingProvider) Configured() bool { return p != nil && p.pool != nil }
func (*StagingProvider) IsSimulation() bool { return true }
func stagingRemoteID(s Snapshot) (string, error) {
	id := "STG-" + s.Code
	if s.SabyID != "" && s.SabyID != id {
		return "", &RejectedError{Message: "На стейджинге можно проверять только новые тестовые карточки; рабочая связь СБИС не изменяется"}
	}
	return id, nil
}
func (p *StagingProvider) Lookup(ctx context.Context, s Snapshot) (*Remote, error) {
	id, err := stagingRemoteID(s)
	if err != nil {
		return nil, err
	}
	var data string
	err = p.pool.QueryRow(ctx, `SELECT description FROM saby_nomenclature WHERE saby_id=$1`, id).Scan(&data)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var remote Remote
	if err = json.Unmarshal([]byte(data), &remote); err != nil {
		return nil, err
	}
	if remote.ID != id || remote.Code != s.Code {
		return nil, errors.New("staging remote identity mismatch")
	}
	return &remote, nil
}
func (p *StagingProvider) Write(ctx context.Context, s Snapshot, withPrice bool) error {
	if err := s.Validate(); err != nil {
		return err
	}
	id, err := stagingRemoteID(s)
	if err != nil {
		return err
	}
	price := s.PriceMinor
	if !withPrice {
		previous, err := p.Lookup(ctx, s)
		if err != nil {
			return err
		}
		if previous == nil {
			return &RejectedError{Message: "Тестовая карточка не найдена"}
		}
		price = previous.PriceMinor
	}
	remote := Remote{ID: id, Code: s.Code, Article: s.SKU, Name: s.Name, PotCM: s.PotCM, HeightCM: s.HeightCM, PriceMinor: price}
	data, err := json.Marshal(remote)
	if err != nil {
		return err
	}
	tag, err := p.pool.Exec(ctx, `INSERT INTO saby_nomenclature(saby_id,code,article,name,description,price_minor,balance) VALUES($1,$2,$3,$4,$5,$6,0) ON CONFLICT(saby_id) DO UPDATE SET article=EXCLUDED.article,name=EXCLUDED.name,description=EXCLUDED.description,price_minor=EXCLUDED.price_minor,balance=0 WHERE saby_nomenclature.code=EXCLUDED.code`, id, s.Code, s.SKU, s.Name, string(data), price)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("staging identity conflict")
	}
	return nil
}
