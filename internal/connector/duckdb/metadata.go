package duckdb

import (
	"context"
	"strings"
	"sync"

	"github.com/wren-engine/wren/internal/connector"
)

// Metadata wraps a Connector and adds init/session SQL state. Mirrors Java
// DuckDBMetadata. The init SQL is executed against a freshly-opened pool; the
// session SQL is appended to each new connection (DuckDB has no native session
// hook, so we currently re-execute on reload — same as Java HikariCP path).
type Metadata struct {
	mu         sync.RWMutex
	conn       *Connector
	initSQL    string
	sessionSQL string
}

// NewMetadata creates a Metadata wrapping a fresh in-memory Connector.
func NewMetadata() *Metadata {
	return &Metadata{conn: NewConnector()}
}

// Close closes the underlying connector.
func (m *Metadata) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.conn == nil {
		return nil
	}
	err := m.conn.Close()
	m.conn = nil
	return err
}

// DirectQuery delegates to the underlying connector.
func (m *Metadata) DirectQuery(ctx context.Context, sql string, params []connector.Parameter) (connector.RecordIterator, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.conn.DirectQuery(ctx, sql, params)
}

// DescribeQuery delegates.
func (m *Metadata) DescribeQuery(ctx context.Context, sql string, params []connector.Parameter) ([]connector.Column, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.conn.DescribeQuery(ctx, sql, params)
}

// DirectDDL delegates.
func (m *Metadata) DirectDDL(ctx context.Context, sql string) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.conn.DirectDDL(ctx, sql)
}

// InitSQL returns the cached init SQL.
func (m *Metadata) InitSQL() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.initSQL
}

// SetInitSQL replaces the init SQL, closes the existing pool, opens a fresh one,
// and re-executes the init SQL. Mirrors Java DuckDBMetadata.setInitSQL+reload.
func (m *Metadata) SetInitSQL(ctx context.Context, sql string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	prev := m.initSQL
	m.initSQL = sql

	if m.conn != nil {
		_ = m.conn.Close()
	}
	m.conn = NewConnector()
	if strings.TrimSpace(sql) != "" {
		if err := m.conn.ExecuteDDL(ctx, sql); err != nil {
			// Roll back to previous state on failure (Java behavior).
			m.initSQL = prev
			_ = m.conn.Close()
			m.conn = NewConnector()
			if strings.TrimSpace(prev) != "" {
				_ = m.conn.ExecuteDDL(ctx, prev)
			}
			return err
		}
	}
	return nil
}

// AppendInitSQL appends sql to init SQL and executes the appended fragment.
func (m *Metadata) AppendInitSQL(ctx context.Context, sql string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.conn.ExecuteDDL(ctx, sql); err != nil {
		return err
	}
	if m.initSQL == "" {
		m.initSQL = sql
	} else {
		m.initSQL = m.initSQL + "\n" + sql
	}
	return nil
}

// SessionSQL returns the cached session SQL.
func (m *Metadata) SessionSQL() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessionSQL
}

// SetSessionSQL replaces the session SQL and re-pools. Java reuses the same
// init-SQL semantics; we mirror by re-executing session SQL via DDL.
func (m *Metadata) SetSessionSQL(ctx context.Context, sql string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	prev := m.sessionSQL
	m.sessionSQL = sql
	if strings.TrimSpace(sql) != "" {
		if err := m.conn.ExecuteDDL(ctx, sql); err != nil {
			m.sessionSQL = prev
			return err
		}
	}
	return nil
}

// AppendSessionSQL appends sql to session SQL and executes the appended fragment.
func (m *Metadata) AppendSessionSQL(ctx context.Context, sql string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.conn.ExecuteDDL(ctx, sql); err != nil {
		return err
	}
	if m.sessionSQL == "" {
		m.sessionSQL = sql
	} else {
		m.sessionSQL = m.sessionSQL + "\n" + sql
	}
	return nil
}
