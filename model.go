/*
 * @Author: kamalyes 501893067@qq.com
 * @Date: 2023-09-25 10:22:08
 * @LastEditors: kamalyes 501893067@qq.com
 * @LastEditTime: 2026-06-09 12:21:26
 * @FilePath: \go-distsaga-gorm-adapter\model.go
 * @Description: GORM 数据模型 - 事务表的 ORM 映射
 *
 * Copyright (c) 2026 by kamalyes, All Rights Reserved.
 */
package gormadapter

import (
	"encoding/json"
	"time"

	"github.com/kamalyes/go-distsaga"
	"gorm.io/gorm"
)

// TransactionModel 事务表 GORM 模型
// 将 distsaga.Transaction 映射到关系型数据库表
type TransactionModel struct {
	ID          string     `gorm:"column:id;type:varchar(64);primaryKey"`  // 事务 ID
	Name        string     `gorm:"column:name;type:varchar(255);not null"` // 事务名称
	Mode        int        `gorm:"column:mode;type:int;not null"`          // 事务模式
	State       int        `gorm:"column:state;type:int;not null"`         // 事务状态
	Steps       string     `gorm:"column:steps;type:text"`                 // SAGA 步骤状态（JSON）
	TCCBranches string     `gorm:"column:tcc_branches;type:text"`          // TCC 分支状态（JSON）
	XABranches  string     `gorm:"column:xa_branches;type:text"`           // XA 分支状态（JSON）
	OutboxMsgs  string     `gorm:"column:outbox_messages;type:text"`       // Outbox 消息状态（JSON）
	Payload     string     `gorm:"column:payload;type:text"`               // 附加数据（JSON）
	CreatedAt   time.Time  `gorm:"column:created_at;autoCreateTime"`       // 创建时间
	UpdatedAt   time.Time  `gorm:"column:updated_at;autoUpdateTime"`       // 更新时间
	FinishedAt  *time.Time `gorm:"column:finished_at"`                     // 结束时间
}

// TableName 指定表名
func (TransactionModel) TableName() string {
	return "distsaga_transactions"
}

// ToModel 将 distsaga.Transaction 转换为 TransactionModel
func ToModel(tx *distsaga.Transaction) (*TransactionModel, error) {
	m := &TransactionModel{
		ID:         tx.ID,
		Name:       tx.Name,
		Mode:       int(tx.Mode),
		State:      int(tx.State),
		CreatedAt:  tx.CreatedAt,
		UpdatedAt:  tx.UpdatedAt,
		FinishedAt: tx.FinishedAt,
	}

	if len(tx.Steps) > 0 {
		data, err := json.Marshal(tx.Steps)
		if err != nil {
			return nil, err
		}
		m.Steps = string(data)
	}
	if len(tx.TCCBranches) > 0 {
		data, err := json.Marshal(tx.TCCBranches)
		if err != nil {
			return nil, err
		}
		m.TCCBranches = string(data)
	}
	if len(tx.XABranches) > 0 {
		data, err := json.Marshal(tx.XABranches)
		if err != nil {
			return nil, err
		}
		m.XABranches = string(data)
	}
	if len(tx.OutboxMsgs) > 0 {
		data, err := json.Marshal(tx.OutboxMsgs)
		if err != nil {
			return nil, err
		}
		m.OutboxMsgs = string(data)
	}
	if len(tx.Payload) > 0 {
		data, err := json.Marshal(tx.Payload)
		if err != nil {
			return nil, err
		}
		m.Payload = string(data)
	}

	return m, nil
}

// ToTransaction 将 TransactionModel 转换为 distsaga.Transaction
func (m *TransactionModel) ToTransaction() (*distsaga.Transaction, error) {
	tx := &distsaga.Transaction{
		ID:         m.ID,
		Name:       m.Name,
		Mode:       distsaga.TxMode(m.Mode),
		State:      distsaga.TxState(m.State),
		Payload:    make(map[string]interface{}),
		CreatedAt:  m.CreatedAt,
		UpdatedAt:  m.UpdatedAt,
		FinishedAt: m.FinishedAt,
	}

	if m.Steps != "" {
		if err := json.Unmarshal([]byte(m.Steps), &tx.Steps); err != nil {
			return nil, err
		}
	}
	if m.TCCBranches != "" {
		if err := json.Unmarshal([]byte(m.TCCBranches), &tx.TCCBranches); err != nil {
			return nil, err
		}
	}
	if m.XABranches != "" {
		if err := json.Unmarshal([]byte(m.XABranches), &tx.XABranches); err != nil {
			return nil, err
		}
	}
	if m.OutboxMsgs != "" {
		if err := json.Unmarshal([]byte(m.OutboxMsgs), &tx.OutboxMsgs); err != nil {
			return nil, err
		}
	}
	if m.Payload != "" {
		if err := json.Unmarshal([]byte(m.Payload), &tx.Payload); err != nil {
			return nil, err
		}
	}

	return tx, nil
}

// AutoMigrate 自动迁移表结构
func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(&TransactionModel{})
}
