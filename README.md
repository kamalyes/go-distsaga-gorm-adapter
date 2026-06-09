# go-distsaga-gorm-adapter

基于 GORM 的 [go-distsaga](https://github.com/kamalyes/go-distsaga) 存储适配器，实现 `FullStoreAdapter` + `TransactionalStoreAdapter` 接口，支持 MySQL / PostgreSQL / SQLite

## 特性

- 实现 `FullStoreAdapter` 接口（StoreAdapter + FilteredStoreAdapter + ContextStoreAdapter）
- 实现 `TransactionalStoreAdapter` 接口（支持在同一数据库事务中执行多个操作）
- 支持 MySQL / PostgreSQL / SQLite
- 自动建表（`WithAutoMigrate` 选项）
- 主键冲突检测（兼容 MySQL / PostgreSQL / SQLite）
- 完整的模型双向转换（`ToModel` / `ToTransaction`）
- 集成 [go-logger](https://github.com/kamalyes/go-logger) 日志，关键操作均有日志输出

## 安装

```bash
go get github.com/kamalyes/go-distsaga-gorm-adapter
```

## 快速开始

### 电商下单 TCC 场景

以下示例模拟一个典型的电商下单流程：创建订单 → Try（冻结库存+冻结余额）→ Confirm（确认扣减）→ Cancel（释放冻结），使用 GORM + MySQL 作为事务存储后端

```go
package main

import (
    "context"
    "fmt"
    "time"

    "github.com/kamalyes/go-distsaga"
    gormadapter "github.com/kamalyes/go-distsaga-gorm-adapter"
    "github.com/kamalyes/go-logger"
    "gorm.io/driver/mysql"
    "gorm.io/gorm"
)

func main() {
    // 1. 创建日志记录器
    log := logger.NewLogger().
        WithLevel(logger.INFO).
        WithPrefix("[ORDER-TCC] ").
        WithShowCaller(false).
        WithColorful(true).
        WithTimeFormat(time.DateTime)

    // 2. 创建 GORM 数据库连接
    db, err := gorm.Open(mysql.Open("order:pass@tcp(127.0.0.1:3306)/distsaga?charset=utf8mb4&parseTime=True"), &gorm.Config{})
    if err != nil {
        log.Fatalf("连接数据库失败: %v", err)
    }

    // 3. 创建 GORM 适配器（注入日志 + 自动建表）
    adapter := gormadapter.New(db,
        gormadapter.WithAutoMigrate(),
        gormadapter.WithLogger(log),
    )

    ctx := context.Background()

    // 4. 创建 TCC 事务
    tx := distsaga.NewTransaction("order-create-tcc", distsaga.ModeTCC)
    tx.Payload = map[string]interface{}{
        "orderId":     "ORD-20260609-001",
        "userId":      "USR-10086",
        "productId":   "SKU-8888",
        "quantity":    2,
        "totalAmount": 299.00,
    }

    // 5. 保存事务
    if err := adapter.SaveTransaction(ctx, tx); err != nil {
        log.Fatalf("保存事务失败: %v", err)
    }
    // 日志输出: [ORDER-TCC] [GormAdapter] SaveTransaction: txID=xxx, name=order-create-tcc, mode=TCC, state=Pending

    // 6. Try 阶段：冻结库存 + 冻结余额
    if err := adapter.UpdateTransactionState(ctx, tx.ID, distsaga.StateTrying, map[string]interface{}{
        "currentStep":    "try-freeze",
        "stockFrozen":    true,
        "balanceFrozen":  true,
    }); err != nil {
        log.Fatalf("Try 阶段失败: %v", err)
    }

    // 7. Confirm 阶段：确认扣减
    if err := adapter.UpdateTransactionState(ctx, tx.ID, distsaga.StateConfirming, map[string]interface{}{
        "currentStep":    "confirm-deduct",
    }); err != nil {
        log.Fatalf("Confirm 阶段失败: %v", err)
    }

    // 8. 提交事务
    if err := adapter.UpdateTransactionState(ctx, tx.ID, distsaga.StateCommitted, map[string]interface{}{
        "stockDeducted":   true,
        "balanceDeducted": true,
    }); err != nil {
        log.Fatalf("提交事务失败: %v", err)
    }

    // 9. 查询最终状态
    loaded, err := adapter.LoadTransaction(ctx, tx.ID)
    if err != nil {
        log.Fatalf("加载事务失败: %v", err)
    }
    fmt.Printf("订单 %s 最终状态: %s\n", loaded.Payload["orderId"], loaded.State)
}
```

### 使用 PostgreSQL

```go
import (
    "gorm.io/driver/postgres"
)

db, err := gorm.Open(postgres.Open("host=127.0.0.1 user=postgres password=postgres dbname=distsaga sslmode=disable"), &gorm.Config{})
adapter := gormadapter.New(db, gormadapter.WithAutoMigrate(), gormadapter.WithLogger(log))
```

### 使用 SQLite

```go
import (
    "gorm.io/driver/sqlite"
)

db, err := gorm.Open(sqlite.Open("distsaga.db"), &gorm.Config{})
adapter := gormadapter.New(db, gormadapter.WithAutoMigrate(), gormadapter.WithLogger(log))
```

### 事务操作：订单 + 库存原子性保证

在同一个数据库事务中保存订单事务和库存事务，确保原子性：

```go
err := adapter.ExecuteInTransaction(ctx, func(sa distsaga.StoreAdapter) error {
    // 保存订单事务
    orderTx := distsaga.NewTransaction("order-create", distsaga.ModeSaga)
    orderTx.Payload = map[string]interface{}{
        "orderId": "ORD-20260609-001",
        "amount":  299.00,
    }
    if err := sa.SaveTransaction(ctx, orderTx); err != nil {
        return err // 返回错误将触发回滚
    }

    // 保存库存事务
    stockTx := distsaga.NewTransaction("stock-deduct", distsaga.ModeSaga)
    stockTx.Payload = map[string]interface{}{
        "productId": "SKU-8888",
        "quantity":  2,
    }
    if err := sa.SaveTransaction(ctx, stockTx); err != nil {
        return err // 返回错误将触发回滚，orderTx 也会回滚
    }

    return nil // 返回 nil 将提交事务
})
```

### 恢复挂起事务

使用 `LoadByState` 扫描需要恢复的事务：

```go
// 扫描所有处于 COMPENSATING 状态的事务（需要补偿恢复）
txs, err := adapter.LoadByState(ctx, distsaga.StateCompensating, 100)
if err != nil {
    log.Fatalf("扫描事务失败: %v", err)
}
for _, tx := range txs {
    log.Warnf("发现待补偿事务: txID=%s, name=%s, orderId=%v", tx.ID, tx.Name, tx.Payload["orderId"])
    // 执行补偿恢复逻辑...
}
```

## 配置选项

| 选项 | 说明 |
|------|------|
| `WithAutoMigrate()` | 创建适配器时自动迁移表结构 |
| `WithLogger(l)` | 设置日志记录器 |

## 日志输出示例

使用 `WithLogger` 注入日志后，关键操作会输出结构化日志：

```bash
2026-06-09 13:26:00 [ORDER-TCC] [GormAdapter] SaveTransaction: txID=distsaga-xxx, name=order-create-tcc, mode=TCC, state=Pending
2026-06-09 13:26:00 [ORDER-TCC] [GormAdapter] UpdateTransactionState: txID=distsaga-xxx, state=Trying, updates=map[currentStep:try-freeze stockFrozen:true balanceFrozen:true]
2026-06-09 13:26:01 [ORDER-TCC] [GormAdapter] UpdateTransactionState: txID=distsaga-xxx, state=Confirming, updates=map[currentStep:confirm-deduct]
2026-06-09 13:26:01 [ORDER-TCC] [GormAdapter] UpdateTransactionState: txID=distsaga-xxx, state=Committed, updates=map[stockDeducted:true balanceDeducted:true]
2026-06-09 13:26:02 [ORDER-TCC] [GormAdapter] ExecuteInTransaction: starting db transaction
2026-06-09 13:26:02 [ORDER-TCC] [GormAdapter] SaveTransaction(in-tx): txID=distsaga-yyy, name=stock-deduct
2026-06-09 13:26:02 [ORDER-TCC] [GormAdapter] ExecuteInTransaction: committed successfully
```

## 数据库表结构

适配器使用 `distsaga_transactions` 表存储事务数据：

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | varchar(64) | 事务 ID（主键） |
| `name` | varchar(255) | 事务名称 |
| `mode` | int | 事务模式（SAGA/TCC/XA/Workflow/Outbox） |
| `state` | int | 事务状态 |
| `steps` | text | SAGA 步骤状态（JSON） |
| `tcc_branches` | text | TCC 分支状态（JSON） |
| `xa_branches` | text | XA 分支状态（JSON） |
| `outbox_messages` | text | Outbox 消息状态（JSON） |
| `payload` | text | 附加数据（JSON） |
| `created_at` | datetime | 创建时间 |
| `updated_at` | datetime | 更新时间 |
| `finished_at` | datetime | 结束时间（终态时设置） |

## 接口实现

| 接口 | 方法 | 说明 |
|------|------|------|
| StoreAdapter | `SaveTransaction` | 保存事务 |
| StoreAdapter | `LoadTransaction` | 加载事务 |
| StoreAdapter | `UpdateTransactionState` | 更新事务状态 |
| StoreAdapter | `DeleteTransaction` | 删除事务 |
| FilteredStoreAdapter | `LoadByState` | 按状态加载事务列表 |
| ContextStoreAdapter | `SaveTransactionWithCtx` | 带上下文保存 |
| ContextStoreAdapter | `LoadTransactionWithCtx` | 带上下文加载 |
| ContextStoreAdapter | `UpdateTransactionStateWithCtx` | 带上下文更新 |
| ContextStoreAdapter | `DeleteTransactionWithCtx` | 带上下文删除 |
| TransactionalStoreAdapter | `ExecuteInTransaction` | 在事务中执行函数 |

## 运行测试

```bash
go test ./... -v
```

测试使用 SQLite 内存数据库，无需安装 MySQL / PostgreSQL

## 依赖

- [go-distsaga](https://github.com/kamalyes/go-distsaga) - 分布式事务框架
- [go-logger](https://github.com/kamalyes/go-logger) - 日志记录器
- [gorm](https://gorm.io) - Go ORM 库

## License

MIT
