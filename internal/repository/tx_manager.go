package repository

import (
	"context"

	"gorm.io/gorm"
)

type txKey struct{}

type gormTxManager struct {
	db *gorm.DB
}

func NewTransactionManager(db *gorm.DB) TransactionManager {
	return &gormTxManager{db: db}
}

func (m *gormTxManager) WithTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	return m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		txCtx := context.WithValue(ctx, txKey{}, tx)
		return fn(txCtx)
	})
}

// GetDB retrieves a gorm.DB instance from context if a active transaction exists; otherwise returns default DB.
func GetDB(ctx context.Context, defaultDB *gorm.DB) *gorm.DB {
	if tx, ok := ctx.Value(txKey{}).(*gorm.DB); ok {
		return tx.WithContext(ctx)
	}
	return defaultDB.WithContext(ctx)
}
