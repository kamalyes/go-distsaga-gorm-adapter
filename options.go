/*
 * @Author: kamalyes 501893067@qq.com
 * @Date: 2023-09-25 10:22:08
 * @LastEditors: kamalyes 501893067@qq.com
 * @LastEditTime: 2026-06-09 12:21:16
 * @FilePath: \go-distsaga-gorm-adapter\options.go
 * @Description: GORM 适配器配置选项
 *
 * Copyright (c) 2026 by kamalyes, All Rights Reserved.
 */
package gormadapter

import "github.com/kamalyes/go-logger"

// Option GORM 适配器配置选项函数
type Option func(*GormAdapter)

// WithAutoMigrate 启用自动迁移
// 创建适配器时自动迁移表结构
func WithAutoMigrate() Option {
	return func(a *GormAdapter) {
		_ = AutoMigrate(a.db)
	}
}

// WithLogger 设置日志记录器
func WithLogger(l logger.ILogger) Option {
	return func(a *GormAdapter) {
		a.logger = l
	}
}
