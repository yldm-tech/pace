package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/yldm-tech/pace/apps/api/internal/config"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

const (
	// Without a lifetime a pooled connection is kept for as long as the process lives, so a Postgres failover or a pgbouncer restart leaves the pool holding backends that are gone and every request that draws one fails until it is drawn again. Half an hour is a rotation that costs almost nothing: a full pool of twenty-five connections retired over thirty minutes is one new backend every seventy-two seconds, and with only five connections kept idle this pool already opens a fresh one whenever more than five requests are in flight at once.
	connectionMaxLifetime = 30 * time.Minute
	// An idle connection is still a backend with its own memory on the database, and the pool keeps five of them. Five minutes releases them on an installation that goes quiet overnight while keeping them through any gap a person's browsing leaves.
	connectionMaxIdleTime = 5 * time.Minute
)

type Connection struct {
	GORM *gorm.DB
	SQL  *sql.DB
}

func Open(ctx context.Context, cfg config.Config) (*Connection, error) {
	db, err := gorm.Open(postgres.Open(cfg.DatabaseURL), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("open PostgreSQL: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("access SQL connection: %w", err)
	}
	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(connectionMaxLifetime)
	sqlDB.SetConnMaxIdleTime(connectionMaxIdleTime)
	if err := sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("ping PostgreSQL: %w", err)
	}
	return &Connection{GORM: db, SQL: sqlDB}, nil
}
