package repository

import (
	"database/sql"
	"errors"
	"log/slog"
	"sync"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
)

const routingBackgroundApplicationName = "sub2api-routing-background"

// RoutingBackgroundDB owns a small connection pool used only by API-key
// routing score/history jobs. It intentionally has a distinct DI type so a
// background query can never silently fall back to the request-serving pool.
// sql.Open is lazy, therefore disabled routing features do not establish an
// extra PostgreSQL connection merely because the application starts.
type RoutingBackgroundDB struct {
	DB     *sql.DB
	Client *dbent.Client

	closeOnce sync.Once
	closeErr  error
}

func ProvideRoutingBackgroundDB(cfg *config.Config) (*RoutingBackgroundDB, error) {
	if cfg == nil {
		return nil, errors.New("nil config for routing background database")
	}
	dsn := cfg.Database.DSNWithTimezone(cfg.Timezone) + " application_name=" + routingBackgroundApplicationName
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}
	maxOpen := cfg.Database.RoutingBackgroundMaxOpenConns
	if maxOpen <= 0 {
		maxOpen = 2
	}
	maxIdle := cfg.Database.RoutingBackgroundMaxIdleConns
	if maxIdle < 0 || maxIdle > maxOpen {
		maxIdle = 1
	}
	db.SetMaxOpenConns(maxOpen)
	db.SetMaxIdleConns(maxIdle)
	settings := clampDBPoolSettings(cfg)
	db.SetConnMaxLifetime(settings.ConnMaxLifetime)
	db.SetConnMaxIdleTime(settings.ConnMaxIdleTime)

	driver := entsql.OpenDB(dialect.Postgres, db)
	pool := &RoutingBackgroundDB{DB: db, Client: dbent.NewClient(dbent.Driver(driver))}
	slog.Info("routing background database pool configured",
		slog.Int("max_open", maxOpen),
		slog.Int("max_idle", maxIdle),
		slog.Duration("query_timeout", time.Duration(cfg.Database.RoutingBackgroundQueryTimeoutSeconds)*time.Second),
	)
	return pool, nil
}

func (p *RoutingBackgroundDB) Close() error {
	if p == nil || p.DB == nil {
		return nil
	}
	p.closeOnce.Do(func() { p.closeErr = p.DB.Close() })
	return p.closeErr
}

func (p *RoutingBackgroundDB) SQLDB() *sql.DB {
	if p == nil {
		return nil
	}
	return p.DB
}
