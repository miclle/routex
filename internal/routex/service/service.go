// Package service provides business logic and database operations.
package service

import (
	"context"
	"fmt"

	"github.com/fox-gonic/fox/logger"
	"gorm.io/gorm"
)

// Service holds the database connection and provides business logic methods.
type Service struct {
	db *gorm.DB
}

// New creates a new Service instance with the given database handle.
func New(ctx context.Context, db *gorm.DB) (*Service, error) {
	l := logger.NewWithContext(ctx)

	if db == nil {
		return nil, fmt.Errorf("db handle is required")
	}

	l.Info("[Service] initialized")

	return &Service{db: db}, nil
}

// DB returns the underlying GORM database connection.
func (s *Service) DB() *gorm.DB {
	return s.db
}
