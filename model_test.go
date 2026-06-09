/*
 * @Author: kamalyes 501893067@qq.com
 * @Date: 2023-09-25 10:22:08
 * @LastEditors: kamalyes 501893067@qq.com
 * @LastEditTime: 2026-06-09 12:16:08
 * @FilePath: \go-distsaga-gorm-adapter\model_test.go
 * @Description: GORM 模型转换测试
 *
 * Copyright (c) 2026 by kamalyes, All Rights Reserved.
 */
package gormadapter

import (
	"testing"
	"time"

	"github.com/kamalyes/go-distsaga"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToModel_BasicFields(t *testing.T) {
	t.Run("基本字段转换", func(t *testing.T) {
		tx := distsaga.NewTransaction("model-test", distsaga.ModeSaga)
		model, err := ToModel(tx)
		require.NoError(t, err)

		assert.Equal(t, tx.ID, model.ID)
		assert.Equal(t, tx.Name, model.Name)
		assert.Equal(t, int(tx.Mode), model.Mode)
		assert.Equal(t, int(tx.State), model.State)
		assert.Equal(t, tx.CreatedAt, model.CreatedAt)
		assert.Equal(t, tx.UpdatedAt, model.UpdatedAt)
	})
}

func TestToModel_WithSteps(t *testing.T) {
	t.Run("带 SAGA 步骤的转换", func(t *testing.T) {
		tx := distsaga.NewTransaction("steps-test", distsaga.ModeSaga)
		tx.Steps = []distsaga.StepStateItem{
			{
				Name:   "step1",
				Status: distsaga.StateRunning,
				Result: distsaga.StepResult{Data: map[string]string{"key": "value"}},
			},
		}

		model, err := ToModel(tx)
		require.NoError(t, err)
		assert.NotEmpty(t, model.Steps)
	})
}

func TestToModel_WithTCCBranches(t *testing.T) {
	t.Run("带 TCC 分支的转换", func(t *testing.T) {
		tx := distsaga.NewTransaction("tcc-test", distsaga.ModeTCC)
		tx.TCCBranches = []distsaga.TCCBranchStateItem{
			{
				Name:   "branch1",
				Status: distsaga.StateTrying,
				TryResult: distsaga.StepResult{
					Data: map[string]string{"reserved": "100"},
				},
			},
		}

		model, err := ToModel(tx)
		require.NoError(t, err)
		assert.NotEmpty(t, model.TCCBranches)
	})
}

func TestToModel_WithPayload(t *testing.T) {
	t.Run("带 Payload 的转换", func(t *testing.T) {
		tx := distsaga.NewTransaction("payload-test", distsaga.ModeSaga)
		tx.Payload = map[string]interface{}{
			"orderId": "ORD-001",
			"amount":  99.9,
		}

		model, err := ToModel(tx)
		require.NoError(t, err)
		assert.NotEmpty(t, model.Payload)
	})
}

func TestToModel_WithFinishedAt(t *testing.T) {
	t.Run("带结束时间的转换", func(t *testing.T) {
		tx := distsaga.NewTransaction("finished-test", distsaga.ModeSaga)
		now := time.Now()
		tx.FinishedAt = &now

		model, err := ToModel(tx)
		require.NoError(t, err)
		assert.NotNil(t, model.FinishedAt)
	})
}

func TestToModel_EmptyFields(t *testing.T) {
	t.Run("空字段不序列化", func(t *testing.T) {
		tx := distsaga.NewTransaction("empty-test", distsaga.ModeSaga)

		model, err := ToModel(tx)
		require.NoError(t, err)
		assert.Empty(t, model.Steps)
		assert.Empty(t, model.TCCBranches)
		assert.Empty(t, model.XABranches)
		assert.Empty(t, model.OutboxMsgs)
		assert.Empty(t, model.Payload)
	})
}

func TestToTransaction_RoundTrip(t *testing.T) {
	t.Run("ToModel -> ToTransaction 往返转换", func(t *testing.T) {
		tx := distsaga.NewTransaction("roundtrip-test", distsaga.ModeSaga)
		tx.Payload = map[string]interface{}{
			"key": "value",
		}

		model, err := ToModel(tx)
		require.NoError(t, err)

		recovered, err := model.ToTransaction()
		require.NoError(t, err)

		assert.Equal(t, tx.ID, recovered.ID)
		assert.Equal(t, tx.Name, recovered.Name)
		assert.Equal(t, tx.Mode, recovered.Mode)
		assert.Equal(t, tx.State, recovered.State)
		assert.Equal(t, "value", recovered.Payload["key"])
	})
}

func TestToTransaction_AllModes(t *testing.T) {
	modes := []struct {
		name string
		mode distsaga.TxMode
	}{
		{"SAGA", distsaga.ModeSaga},
		{"TCC", distsaga.ModeTCC},
		{"XA", distsaga.ModeXA},
		{"Workflow", distsaga.ModeWorkflow},
		{"Outbox", distsaga.ModeOutbox},
	}

	for _, tc := range modes {
		t.Run(tc.name+"模式往返转换", func(t *testing.T) {
			tx := distsaga.NewTransaction(tc.name+"-test", tc.mode)

			model, err := ToModel(tx)
			require.NoError(t, err)

			recovered, err := model.ToTransaction()
			require.NoError(t, err)
			assert.Equal(t, tc.mode, recovered.Mode)
		})
	}
}

func TestTransactionModel_TableName(t *testing.T) {
	t.Run("表名验证", func(t *testing.T) {
		model := TransactionModel{}
		assert.Equal(t, "distsaga_transactions", model.TableName())
	})
}
