/*
 * @Author: kamalyes 501893067@qq.com
 * @Date: 2026-05-25 10:22:08
 * @LastEditors: kamalyes 501893067@qq.com
 * @LastEditTime: 2026-06-09 12:22:16
 * @FilePath: \go-distsaga-gorm-adapter\adapter_test.go
 * @Description: GORM 适配器测试 - 使用 SQLite 内存数据库
 *
 * Copyright (c) 2026 by kamalyes, All Rights Reserved.
 */
package gormadapter

import (
	"context"
	"testing"

	"github.com/kamalyes/go-distsaga"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// setupTestDB 创建测试用的 SQLite 内存数据库
func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	err = AutoMigrate(db)
	require.NoError(t, err)

	return db
}

func TestGormAdapter_ImplementsInterfaces(t *testing.T) {
	t.Run("GormAdapter 实现 FullStoreAdapter", func(t *testing.T) {
		db := setupTestDB(t)
		adapter := New(db)
		var _ distsaga.FullStoreAdapter = adapter
	})

	t.Run("GormAdapter 实现 TransactionalStoreAdapter", func(t *testing.T) {
		db := setupTestDB(t)
		adapter := New(db)
		var _ distsaga.TransactionalStoreAdapter = adapter
	})
}

func TestGormAdapter_SaveAndLoad(t *testing.T) {
	ctx := context.Background()
	db := setupTestDB(t)
	adapter := New(db)

	t.Run("保存和加载事务", func(t *testing.T) {
		tx := distsaga.NewTransaction("test-saga", distsaga.ModeSaga)

		err := adapter.SaveTransaction(ctx, tx)
		require.NoError(t, err)

		loaded, err := adapter.LoadTransaction(ctx, tx.ID)
		require.NoError(t, err)
		assert.Equal(t, tx.ID, loaded.ID)
		assert.Equal(t, tx.Name, loaded.Name)
		assert.Equal(t, tx.Mode, loaded.Mode)
		assert.Equal(t, tx.State, loaded.State)
	})

	t.Run("加载不存在的事务", func(t *testing.T) {
		_, err := adapter.LoadTransaction(ctx, "nonexistent")
		assert.Error(t, err)
		assert.ErrorIs(t, err, distsaga.ErrTransactionNotFound)
	})

	t.Run("重复保存事务", func(t *testing.T) {
		tx := distsaga.NewTransaction("dup-test", distsaga.ModeSaga)
		err := adapter.SaveTransaction(ctx, tx)
		require.NoError(t, err)

		err = adapter.SaveTransaction(ctx, tx)
		assert.Error(t, err)
		assert.ErrorIs(t, err, distsaga.ErrDuplicateOperation)
	})
}

func TestGormAdapter_UpdateTransactionState(t *testing.T) {
	ctx := context.Background()
	db := setupTestDB(t)
	adapter := New(db)

	t.Run("更新事务状态", func(t *testing.T) {
		tx := distsaga.NewTransaction("update-test", distsaga.ModeSaga)
		require.NoError(t, adapter.SaveTransaction(ctx, tx))

		err := adapter.UpdateTransactionState(ctx, tx.ID, distsaga.StateRunning, nil)
		require.NoError(t, err)

		loaded, err := adapter.LoadTransaction(ctx, tx.ID)
		require.NoError(t, err)
		assert.Equal(t, distsaga.StateRunning, loaded.State)
	})

	t.Run("更新不存在的事务", func(t *testing.T) {
		err := adapter.UpdateTransactionState(ctx, "nonexistent", distsaga.StateRunning, nil)
		assert.Error(t, err)
		assert.ErrorIs(t, err, distsaga.ErrTransactionNotFound)
	})

	t.Run("更新事务状态并附加数据", func(t *testing.T) {
		tx := distsaga.NewTransaction("payload-test", distsaga.ModeSaga)
		require.NoError(t, adapter.SaveTransaction(ctx, tx))

		updates := map[string]interface{}{"key": "value"}
		err := adapter.UpdateTransactionState(ctx, tx.ID, distsaga.StateRunning, updates)
		require.NoError(t, err)

		loaded, err := adapter.LoadTransaction(ctx, tx.ID)
		require.NoError(t, err)
		assert.Equal(t, distsaga.StateRunning, loaded.State)
		assert.Equal(t, "value", loaded.Payload["key"])
	})

	t.Run("非法状态转换返回错误", func(t *testing.T) {
		tx := distsaga.NewTransaction("invalid-trans", distsaga.ModeSaga)
		require.NoError(t, adapter.SaveTransaction(ctx, tx))

		err := adapter.UpdateTransactionState(ctx, tx.ID, distsaga.StateCommitted, nil)
		assert.Error(t, err)
		assert.ErrorIs(t, err, distsaga.ErrStateTransition)
	})

	t.Run("更新到终态时设置 FinishedAt", func(t *testing.T) {
		tx := distsaga.NewTransaction("final-test", distsaga.ModeSaga)
		require.NoError(t, adapter.SaveTransaction(ctx, tx))
		require.NoError(t, adapter.UpdateTransactionState(ctx, tx.ID, distsaga.StateRunning, nil))

		err := adapter.UpdateTransactionState(ctx, tx.ID, distsaga.StateCommitted, nil)
		require.NoError(t, err)

		loaded, err := adapter.LoadTransaction(ctx, tx.ID)
		require.NoError(t, err)
		assert.NotNil(t, loaded.FinishedAt)
		assert.True(t, loaded.IsFinal())
	})
}

func TestGormAdapter_DeleteTransaction(t *testing.T) {
	ctx := context.Background()
	db := setupTestDB(t)
	adapter := New(db)

	t.Run("删除事务", func(t *testing.T) {
		tx := distsaga.NewTransaction("del-test", distsaga.ModeSaga)
		require.NoError(t, adapter.SaveTransaction(ctx, tx))

		err := adapter.DeleteTransaction(ctx, tx.ID)
		require.NoError(t, err)

		_, err = adapter.LoadTransaction(ctx, tx.ID)
		assert.Error(t, err)
		assert.ErrorIs(t, err, distsaga.ErrTransactionNotFound)
	})

	t.Run("删除不存在的事务（不报错）", func(t *testing.T) {
		err := adapter.DeleteTransaction(ctx, "nonexistent")
		assert.NoError(t, err)
	})
}

func TestGormAdapter_LoadByState(t *testing.T) {
	ctx := context.Background()
	db := setupTestDB(t)
	adapter := New(db)

	t.Run("按状态加载事务", func(t *testing.T) {
		tx1 := distsaga.NewTransaction("pending-1", distsaga.ModeSaga)
		tx2 := distsaga.NewTransaction("pending-2", distsaga.ModeSaga)
		tx3 := distsaga.NewTransaction("running-1", distsaga.ModeSaga)
		tx3.State = distsaga.StateRunning

		require.NoError(t, adapter.SaveTransaction(ctx, tx1))
		require.NoError(t, adapter.SaveTransaction(ctx, tx2))
		require.NoError(t, adapter.SaveTransaction(ctx, tx3))

		pendingTxs, err := adapter.LoadByState(ctx, distsaga.StatePending, 0)
		require.NoError(t, err)
		assert.Len(t, pendingTxs, 2)

		runningTxs, err := adapter.LoadByState(ctx, distsaga.StateRunning, 0)
		require.NoError(t, err)
		assert.Len(t, runningTxs, 1)
	})

	t.Run("按状态加载事务（限制数量）", func(t *testing.T) {
		db := setupTestDB(t)
		adapter := New(db)
		for i := 0; i < 5; i++ {
			tx := distsaga.NewTransaction("limit-test", distsaga.ModeSaga)
			require.NoError(t, adapter.SaveTransaction(ctx, tx))
		}

		txs, err := adapter.LoadByState(ctx, distsaga.StatePending, 3)
		require.NoError(t, err)
		assert.Len(t, txs, 3)
	})

	t.Run("按状态加载空结果", func(t *testing.T) {
		db := setupTestDB(t)
		adapter := New(db)
		txs, err := adapter.LoadByState(ctx, distsaga.StateCommitted, 0)
		require.NoError(t, err)
		assert.Len(t, txs, 0)
	})
}

func TestGormAdapter_WithCtx(t *testing.T) {
	ctx := context.Background()
	db := setupTestDB(t)
	adapter := New(db)

	t.Run("SaveTransactionWithCtx", func(t *testing.T) {
		tx := distsaga.NewTransaction("ctx-save", distsaga.ModeSaga)
		err := adapter.SaveTransactionWithCtx(ctx, tx)
		require.NoError(t, err)
	})

	t.Run("LoadTransactionWithCtx", func(t *testing.T) {
		tx := distsaga.NewTransaction("ctx-load", distsaga.ModeSaga)
		require.NoError(t, adapter.SaveTransactionWithCtx(ctx, tx))

		loaded, err := adapter.LoadTransactionWithCtx(ctx, tx.ID)
		require.NoError(t, err)
		assert.Equal(t, tx.ID, loaded.ID)
	})

	t.Run("UpdateTransactionStateWithCtx", func(t *testing.T) {
		tx := distsaga.NewTransaction("ctx-update", distsaga.ModeSaga)
		require.NoError(t, adapter.SaveTransactionWithCtx(ctx, tx))

		err := adapter.UpdateTransactionStateWithCtx(ctx, tx.ID, distsaga.StateRunning, nil)
		require.NoError(t, err)
	})

	t.Run("DeleteTransactionWithCtx", func(t *testing.T) {
		tx := distsaga.NewTransaction("ctx-delete", distsaga.ModeSaga)
		require.NoError(t, adapter.SaveTransactionWithCtx(ctx, tx))

		err := adapter.DeleteTransactionWithCtx(ctx, tx.ID)
		require.NoError(t, err)
	})
}

func TestGormAdapter_ExecuteInTransaction(t *testing.T) {
	ctx := context.Background()
	db := setupTestDB(t)
	adapter := New(db)

	t.Run("在事务中执行多个操作", func(t *testing.T) {
		tx1 := distsaga.NewTransaction("tx-in-tx-1", distsaga.ModeSaga)
		tx2 := distsaga.NewTransaction("tx-in-tx-2", distsaga.ModeTCC)

		err := adapter.ExecuteInTransaction(ctx, func(sa distsaga.StoreAdapter) error {
			if err := sa.SaveTransaction(ctx, tx1); err != nil {
				return err
			}
			return sa.SaveTransaction(ctx, tx2)
		})
		require.NoError(t, err)

		loaded1, err := adapter.LoadTransaction(ctx, tx1.ID)
		require.NoError(t, err)
		assert.Equal(t, tx1.ID, loaded1.ID)

		loaded2, err := adapter.LoadTransaction(ctx, tx2.ID)
		require.NoError(t, err)
		assert.Equal(t, tx2.ID, loaded2.ID)
	})

	t.Run("事务中发生错误时回滚", func(t *testing.T) {
		tx := distsaga.NewTransaction("rollback-test", distsaga.ModeSaga)

		err := adapter.ExecuteInTransaction(ctx, func(sa distsaga.StoreAdapter) error {
			if err := sa.SaveTransaction(ctx, tx); err != nil {
				return err
			}
			// 模拟业务错误
			return distsaga.ErrCompensationFailed
		})
		assert.Error(t, err)

		// 事务回滚，数据不应存在
		_, err = adapter.LoadTransaction(ctx, tx.ID)
		assert.Error(t, err)
		assert.ErrorIs(t, err, distsaga.ErrTransactionNotFound)
	})

	t.Run("事务中的状态更新", func(t *testing.T) {
		tx := distsaga.NewTransaction("tx-update-in-tx", distsaga.ModeSaga)
		require.NoError(t, adapter.SaveTransaction(ctx, tx))

		err := adapter.ExecuteInTransaction(ctx, func(sa distsaga.StoreAdapter) error {
			return sa.UpdateTransactionState(ctx, tx.ID, distsaga.StateRunning, map[string]interface{}{"source": "tx"})
		})
		require.NoError(t, err)

		loaded, err := adapter.LoadTransaction(ctx, tx.ID)
		require.NoError(t, err)
		assert.Equal(t, distsaga.StateRunning, loaded.State)
		assert.Equal(t, "tx", loaded.Payload["source"])
	})
}

func TestGormAdapter_MultipleModes(t *testing.T) {
	ctx := context.Background()
	db := setupTestDB(t)
	adapter := New(db)

	t.Run("SAGA 模式事务完整生命周期", func(t *testing.T) {
		tx := distsaga.NewTransaction("saga-lifecycle", distsaga.ModeSaga)
		require.NoError(t, adapter.SaveTransaction(ctx, tx))

		require.NoError(t, adapter.UpdateTransactionState(ctx, tx.ID, distsaga.StateRunning, nil))
		require.NoError(t, adapter.UpdateTransactionState(ctx, tx.ID, distsaga.StateCommitted, nil))

		loaded, err := adapter.LoadTransaction(ctx, tx.ID)
		require.NoError(t, err)
		assert.Equal(t, distsaga.StateCommitted, loaded.State)
		assert.True(t, loaded.IsFinal())
	})

	t.Run("TCC 模式事务完整生命周期", func(t *testing.T) {
		tx := distsaga.NewTransaction("tcc-lifecycle", distsaga.ModeTCC)
		require.NoError(t, adapter.SaveTransaction(ctx, tx))

		require.NoError(t, adapter.UpdateTransactionState(ctx, tx.ID, distsaga.StateTrying, nil))
		require.NoError(t, adapter.UpdateTransactionState(ctx, tx.ID, distsaga.StateConfirming, nil))
		require.NoError(t, adapter.UpdateTransactionState(ctx, tx.ID, distsaga.StateCommitted, nil))

		loaded, err := adapter.LoadTransaction(ctx, tx.ID)
		require.NoError(t, err)
		assert.Equal(t, distsaga.StateCommitted, loaded.State)
	})

	t.Run("XA 模式事务完整生命周期", func(t *testing.T) {
		tx := distsaga.NewTransaction("xa-lifecycle", distsaga.ModeXA)
		require.NoError(t, adapter.SaveTransaction(ctx, tx))

		require.NoError(t, adapter.UpdateTransactionState(ctx, tx.ID, distsaga.StatePreparing, nil))
		require.NoError(t, adapter.UpdateTransactionState(ctx, tx.ID, distsaga.StatePrepared, nil))
		require.NoError(t, adapter.UpdateTransactionState(ctx, tx.ID, distsaga.StateCommitting, nil))
		require.NoError(t, adapter.UpdateTransactionState(ctx, tx.ID, distsaga.StateCommitted, nil))

		loaded, err := adapter.LoadTransaction(ctx, tx.ID)
		require.NoError(t, err)
		assert.Equal(t, distsaga.StateCommitted, loaded.State)
	})

	t.Run("Outbox 模式事务完整生命周期", func(t *testing.T) {
		tx := distsaga.NewTransaction("outbox-lifecycle", distsaga.ModeOutbox)
		require.NoError(t, adapter.SaveTransaction(ctx, tx))

		require.NoError(t, adapter.UpdateTransactionState(ctx, tx.ID, distsaga.StateSubmitting, nil))
		require.NoError(t, adapter.UpdateTransactionState(ctx, tx.ID, distsaga.StateSubmitted, nil))
		require.NoError(t, adapter.UpdateTransactionState(ctx, tx.ID, distsaga.StateCommitted, nil))

		loaded, err := adapter.LoadTransaction(ctx, tx.ID)
		require.NoError(t, err)
		assert.Equal(t, distsaga.StateCommitted, loaded.State)
	})
}

func TestGormAdapter_WithAutoMigrate(t *testing.T) {
	t.Run("WithAutoMigrate 选项自动建表", func(t *testing.T) {
		db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
		require.NoError(t, err)

		adapter := New(db, WithAutoMigrate())
		assert.NotNil(t, adapter)
	})
}

func TestGormAdapter_DB(t *testing.T) {
	t.Run("获取底层 GORM 实例", func(t *testing.T) {
		db := setupTestDB(t)
		adapter := New(db)
		assert.Equal(t, db, adapter.DB())
	})
}
