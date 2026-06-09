/*
 * @Author: kamalyes 501893067@qq.com
 * @Date: 2023-09-25 10:22:08
 * @LastEditors: kamalyes 501893067@qq.com
 * @LastEditTime: 2026-06-09 12:16:08
 * @FilePath: \go-distsaga-gorm-adapter\adapter.go
 * @Description: 基于 GORM 的分布式事务存储适配器，实现 FullStoreAdapter + TransactionalStoreAdapter 接口
 *               支持 MySQL / PostgreSQL / SQLite
 *
 * Copyright (c) 2026 by kamalyes, All Rights Reserved.
 */
package gormadapter

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/kamalyes/go-distsaga"
	"github.com/kamalyes/go-logger"
	"gorm.io/gorm"
)

// 编译时检查 GormAdapter 实现了 distsaga.FullStoreAdapter 接口
var _ distsaga.FullStoreAdapter = (*GormAdapter)(nil)

// 编译时检查 GormAdapter 实现了 distsaga.TransactionalStoreAdapter 接口
var _ distsaga.TransactionalStoreAdapter = (*GormAdapter)(nil)

// GormAdapter 基于 GORM 的事务存储适配器
// 支持 MySQL / PostgreSQL / SQLite，事务操作在同一个数据库事务中执行
type GormAdapter struct {
	db     *gorm.DB
	logger logger.ILogger
}

// New 创建 GORM 适配器
// db: GORM 数据库实例
// opts: 可选配置
func New(db *gorm.DB, opts ...Option) *GormAdapter {
	a := &GormAdapter{
		db:     db,
		logger: logger.NewEmptyLogger(),
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// DB 获取底层 GORM 数据库实例（用于自定义查询等场景）
func (a *GormAdapter) DB() *gorm.DB {
	return a.db
}

// SaveTransaction 保存事务到数据库
// 事务 ID 重复时返回 ErrDuplicateOperation
func (a *GormAdapter) SaveTransaction(ctx context.Context, tx *distsaga.Transaction) error {
	model, err := ToModel(tx)
	if err != nil {
		a.logger.Errorf("[GormAdapter] SaveTransaction.ToModel failed: txID=%s, err=%v", tx.ID, err)
		return distsaga.WrapAdapterError("SaveTransaction.ToModel", err)
	}

	result := a.db.WithContext(ctx).Create(model)
	if result.Error != nil {
		// 检查是否为主键冲突
		if isDuplicateKeyError(result.Error) {
			a.logger.Warnf("[GormAdapter] SaveTransaction: duplicate txID=%s", tx.ID)
			return fmt.Errorf("%w: transaction %s already exists", distsaga.ErrDuplicateOperation, tx.ID)
		}
		a.logger.Errorf("[GormAdapter] SaveTransaction.Create failed: txID=%s, err=%v", tx.ID, result.Error)
		return distsaga.WrapAdapterError("SaveTransaction.Create", result.Error)
	}
	a.logger.Infof("[GormAdapter] SaveTransaction: txID=%s, name=%s, mode=%s, state=%s", tx.ID, tx.Name, tx.Mode, tx.State)
	return nil
}

// LoadTransaction 从数据库加载事务
// 事务不存在时返回 ErrTransactionNotFound
func (a *GormAdapter) LoadTransaction(ctx context.Context, txID string) (*distsaga.Transaction, error) {
	var model TransactionModel
	result := a.db.WithContext(ctx).Where("id = ?", txID).First(&model)
	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			a.logger.Warnf("[GormAdapter] LoadTransaction: not found txID=%s", txID)
			return nil, fmt.Errorf("%w: %s", distsaga.ErrTransactionNotFound, txID)
		}
		a.logger.Errorf("[GormAdapter] LoadTransaction.First failed: txID=%s, err=%v", txID, result.Error)
		return nil, distsaga.WrapAdapterError("LoadTransaction.First", result.Error)
	}
	tx, err := model.ToTransaction()
	if err != nil {
		a.logger.Errorf("[GormAdapter] LoadTransaction.ToTransaction failed: txID=%s, err=%v", txID, err)
		return nil, err
	}
	a.logger.Infof("[GormAdapter] LoadTransaction: txID=%s, name=%s, state=%s", tx.ID, tx.Name, tx.State)
	return tx, nil
}

// UpdateTransactionState 更新数据库中的事务状态
func (a *GormAdapter) UpdateTransactionState(ctx context.Context, txID string, state distsaga.TxState, updates map[string]interface{}) error {
	var model TransactionModel
	result := a.db.WithContext(ctx).Where("id = ?", txID).First(&model)
	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			a.logger.Warnf("[GormAdapter] UpdateTransactionState: not found txID=%s", txID)
			return fmt.Errorf("%w: %s", distsaga.ErrTransactionNotFound, txID)
		}
		a.logger.Errorf("[GormAdapter] UpdateTransactionState.Find failed: txID=%s, err=%v", txID, result.Error)
		return distsaga.WrapAdapterError("UpdateTransactionState.Find", result.Error)
	}

	// 状态转换校验
	tx, err := model.ToTransaction()
	if err != nil {
		a.logger.Errorf("[GormAdapter] UpdateTransactionState.ToTransaction failed: txID=%s, err=%v", txID, err)
		return distsaga.WrapAdapterError("UpdateTransactionState.ToTransaction", err)
	}
	if err := tx.TransitionTo(state); err != nil {
		a.logger.Errorf("[GormAdapter] UpdateTransactionState.TransitionTo failed: txID=%s, from=%s, to=%s, err=%v", txID, tx.State, state, err)
		return err
	}

	// 合并 payload
	payload := tx.Payload
	if payload == nil {
		payload = make(map[string]interface{})
	}
	for k, v := range updates {
		payload[k] = v
	}
	payloadData, err := jsonMarshal(payload)
	if err != nil {
		a.logger.Errorf("[GormAdapter] UpdateTransactionState.MarshalPayload failed: txID=%s, err=%v", txID, err)
		return distsaga.WrapAdapterError("UpdateTransactionState.MarshalPayload", err)
	}

	// 更新数据库
	updateFields := map[string]interface{}{
		"state":      int(state),
		"payload":    payloadData,
		"updated_at": time.Now(),
	}
	if state.IsFinal() {
		now := time.Now()
		updateFields["finished_at"] = now
	}

	result = a.db.WithContext(ctx).Model(&TransactionModel{}).Where("id = ?", txID).Updates(updateFields)
	if result.Error != nil {
		a.logger.Errorf("[GormAdapter] UpdateTransactionState.Updates failed: txID=%s, err=%v", txID, result.Error)
		return distsaga.WrapAdapterError("UpdateTransactionState.Updates", result.Error)
	}
	a.logger.Infof("[GormAdapter] UpdateTransactionState: txID=%s, state=%s, updates=%v", txID, state, updates)
	return nil
}

// DeleteTransaction 从数据库删除事务
func (a *GormAdapter) DeleteTransaction(ctx context.Context, txID string) error {
	result := a.db.WithContext(ctx).Where("id = ?", txID).Delete(&TransactionModel{})
	if result.Error != nil {
		a.logger.Errorf("[GormAdapter] DeleteTransaction failed: txID=%s, err=%v", txID, result.Error)
		return distsaga.WrapAdapterError("DeleteTransaction", result.Error)
	}
	a.logger.Infof("[GormAdapter] DeleteTransaction: txID=%s", txID)
	return nil
}

// SaveTransactionWithCtx 带上下文保存事务
func (a *GormAdapter) SaveTransactionWithCtx(ctx context.Context, tx *distsaga.Transaction) error {
	return a.SaveTransaction(ctx, tx)
}

// LoadTransactionWithCtx 带上下文加载事务
func (a *GormAdapter) LoadTransactionWithCtx(ctx context.Context, txID string) (*distsaga.Transaction, error) {
	return a.LoadTransaction(ctx, txID)
}

// UpdateTransactionStateWithCtx 带上下文更新事务状态
func (a *GormAdapter) UpdateTransactionStateWithCtx(ctx context.Context, txID string, state distsaga.TxState, updates map[string]interface{}) error {
	return a.UpdateTransactionState(ctx, txID, state, updates)
}

// DeleteTransactionWithCtx 带上下文删除事务
func (a *GormAdapter) DeleteTransactionWithCtx(ctx context.Context, txID string) error {
	return a.DeleteTransaction(ctx, txID)
}

// LoadByState 按状态加载事务列表
// limit > 0 时限制返回数量
func (a *GormAdapter) LoadByState(ctx context.Context, state distsaga.TxState, limit int) ([]*distsaga.Transaction, error) {
	a.logger.Infof("[GormAdapter] LoadByState: state=%s, limit=%d", state, limit)

	query := a.db.WithContext(ctx).Where("state = ?", int(state)).Order("created_at ASC")
	if limit > 0 {
		query = query.Limit(limit)
	}

	var models []TransactionModel
	result := query.Find(&models)
	if result.Error != nil {
		a.logger.Errorf("[GormAdapter] LoadByState.Find failed: state=%s, err=%v", state, result.Error)
		return nil, distsaga.WrapAdapterError("LoadByState.Find", result.Error)
	}

	transactions := make([]*distsaga.Transaction, 0, len(models))
	for i := range models {
		tx, err := models[i].ToTransaction()
		if err != nil {
			a.logger.Warnf("[GormAdapter] LoadByState.ToTransaction skipped: id=%s, err=%v", models[i].ID, err)
			continue
		}
		transactions = append(transactions, tx)
	}
	a.logger.Infof("[GormAdapter] LoadByState: found %d transactions with state=%s", len(transactions), state)
	return transactions, nil
}

// ExecuteInTransaction 在数据库事务中执行函数
// 实现 TransactionalStoreAdapter 接口
// fn 接收的 StoreAdapter 是一个受限于当前 DB 事务的适配器
func (a *GormAdapter) ExecuteInTransaction(ctx context.Context, fn func(distsaga.StoreAdapter) error) error {
	a.logger.Infof("[GormAdapter] ExecuteInTransaction: starting db transaction")
	err := a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		txAdapter := &txScopedAdapter{db: tx, logger: a.logger}
		return fn(txAdapter)
	})
	if err != nil {
		a.logger.Errorf("[GormAdapter] ExecuteInTransaction failed: err=%v", err)
		return err
	}
	a.logger.Infof("[GormAdapter] ExecuteInTransaction: committed successfully")
	return nil
}

// ==================== 事务作用域适配器 ====================

// txScopedAdapter 事务作用域适配器
// 在 GORM 事务内执行 CRUD 操作，确保所有操作在同一个数据库事务中
type txScopedAdapter struct {
	db     *gorm.DB
	logger logger.ILogger
}

// SaveTransaction 在事务中保存事务
func (a *txScopedAdapter) SaveTransaction(ctx context.Context, tx *distsaga.Transaction) error {
	model, err := ToModel(tx)
	if err != nil {
		a.logger.Errorf("[GormAdapter] SaveTransaction.ToModel failed: txID=%s, err=%v", tx.ID, err)
		return distsaga.WrapAdapterError("SaveTransaction.ToModel", err)
	}
	result := a.db.WithContext(ctx).Create(model)
	if result.Error != nil {
		if isDuplicateKeyError(result.Error) {
			a.logger.Warnf("[GormAdapter] SaveTransaction: duplicate txID=%s", tx.ID)
			return fmt.Errorf("%w: transaction %s already exists", distsaga.ErrDuplicateOperation, tx.ID)
		}
		a.logger.Errorf("[GormAdapter] SaveTransaction.Create failed: txID=%s, err=%v", tx.ID, result.Error)
		return distsaga.WrapAdapterError("SaveTransaction.Create", result.Error)
	}
	a.logger.Infof("[GormAdapter] SaveTransaction(in-tx): txID=%s, name=%s", tx.ID, tx.Name)
	return nil
}

// LoadTransaction 在事务中加载事务
func (a *txScopedAdapter) LoadTransaction(ctx context.Context, txID string) (*distsaga.Transaction, error) {
	var model TransactionModel
	result := a.db.WithContext(ctx).Where("id = ?", txID).First(&model)
	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			a.logger.Warnf("[GormAdapter] LoadTransaction(in-tx): not found txID=%s", txID)
			return nil, fmt.Errorf("%w: %s", distsaga.ErrTransactionNotFound, txID)
		}
		a.logger.Errorf("[GormAdapter] LoadTransaction.First failed: txID=%s, err=%v", txID, result.Error)
		return nil, distsaga.WrapAdapterError("LoadTransaction.First", result.Error)
	}
	return model.ToTransaction()
}

// UpdateTransactionState 在事务中更新事务状态
func (a *txScopedAdapter) UpdateTransactionState(ctx context.Context, txID string, state distsaga.TxState, updates map[string]interface{}) error {
	var model TransactionModel
	result := a.db.WithContext(ctx).Where("id = ?", txID).First(&model)
	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			a.logger.Warnf("[GormAdapter] UpdateTransactionState(in-tx): not found txID=%s", txID)
			return fmt.Errorf("%w: %s", distsaga.ErrTransactionNotFound, txID)
		}
		a.logger.Errorf("[GormAdapter] UpdateTransactionState.Find failed: txID=%s, err=%v", txID, result.Error)
		return distsaga.WrapAdapterError("UpdateTransactionState.Find", result.Error)
	}

	tx, err := model.ToTransaction()
	if err != nil {
		a.logger.Errorf("[GormAdapter] UpdateTransactionState.ToTransaction failed: txID=%s, err=%v", txID, err)
		return distsaga.WrapAdapterError("UpdateTransactionState.ToTransaction", err)
	}
	if err := tx.TransitionTo(state); err != nil {
		a.logger.Errorf("[GormAdapter] UpdateTransactionState.TransitionTo failed: txID=%s, from=%s, to=%s, err=%v", txID, tx.State, state, err)
		return err
	}

	payload := tx.Payload
	if payload == nil {
		payload = make(map[string]interface{})
	}
	for k, v := range updates {
		payload[k] = v
	}
	payloadData, err := jsonMarshal(payload)
	if err != nil {
		a.logger.Errorf("[GormAdapter] UpdateTransactionState.MarshalPayload failed: txID=%s, err=%v", txID, err)
		return distsaga.WrapAdapterError("UpdateTransactionState.MarshalPayload", err)
	}

	updateFields := map[string]interface{}{
		"state":      int(state),
		"payload":    payloadData,
		"updated_at": time.Now(),
	}
	if state.IsFinal() {
		now := time.Now()
		updateFields["finished_at"] = now
	}

	result = a.db.WithContext(ctx).Model(&TransactionModel{}).Where("id = ?", txID).Updates(updateFields)
	if result.Error != nil {
		a.logger.Errorf("[GormAdapter] UpdateTransactionState.Updates failed: txID=%s, err=%v", txID, result.Error)
		return distsaga.WrapAdapterError("UpdateTransactionState.Updates", result.Error)
	}
	a.logger.Infof("[GormAdapter] UpdateTransactionState(in-tx): txID=%s, state=%s", txID, state)
	return nil
}

// DeleteTransaction 在事务中删除事务
func (a *txScopedAdapter) DeleteTransaction(ctx context.Context, txID string) error {
	result := a.db.WithContext(ctx).Where("id = ?", txID).Delete(&TransactionModel{})
	if result.Error != nil {
		a.logger.Errorf("[GormAdapter] DeleteTransaction(in-tx) failed: txID=%s, err=%v", txID, result.Error)
		return distsaga.WrapAdapterError("DeleteTransaction", result.Error)
	}
	a.logger.Infof("[GormAdapter] DeleteTransaction(in-tx): txID=%s", txID)
	return nil
}

// ==================== 辅助函数 ====================

// jsonMarshal JSON 序列化辅助
func jsonMarshal(v interface{}) (string, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// isDuplicateKeyError 判断是否为主键冲突错误
// 兼容 MySQL / PostgreSQL / SQLite 的错误码
func isDuplicateKeyError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	// MySQL: Error 1062
	// PostgreSQL: duplicate key value violates unique constraint
	// SQLite: UNIQUE constraint failed
	return contains(msg, "1062") ||
		contains(msg, "duplicate key") ||
		contains(msg, "UNIQUE constraint failed")
}

// contains 字符串包含检查
func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
