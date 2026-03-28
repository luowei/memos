# Memo 导出与兰空附件同步功能实现与升级指南

本文档记录当前仓库中两个增强功能的实现细节、数据流、改动位置、升级兼容注意事项与回归验证方法，供后续主仓库升级或重大重构后快速恢复兼容使用。

适用功能：

1. Memo 导出 Markdown 到指定目录
2. 扫描 Memo 附件并同步到兰空图床（Lsky）

关联文档：

- [Lsky 2.1 API 文档](./lsky-2.1-api-doc.md)

## 1. 功能概览

### 1.1 Memo 导出 Markdown

目标：

- 从当前登录用户的全部 memo 中导出 Markdown 文件
- 导出目录可由前端设置页输入
- 每篇 memo 导出为单独 `.md` 文件
- 内容为适配 Jekyll posts 的 front matter + memo 正文
- 导出后为该 memo 记录最近一次导出时间
- 若 memo 曾导出过，则在 memo 时间栏后显示 `export_time`

当前接口：

- `POST /api/v1/memos/export-markdown`

### 1.2 兰空附件同步

目标：

- 扫描当前登录用户全部 memo
- 只处理“有 attachment 记录”的 memo
- 图片附件直接上传到 Lsky
- PDF 附件尝试转换成图片后上传
- 压缩包或其他不可处理附件整条 memo 跳过
- 上传成功后将图片 Markdown 链接追加到 memo 正文末尾
- 避免重复追加

当前接口：

- `POST /api/v1/memos/sync-attachments-to-lsky`

## 2. 关键实现文件

### 2.1 后端

- `server/router/api/v1/memo_service_export.go`
  - Memo 导出核心逻辑
- `server/router/api/v1/memo_service_lsky_sync.go`
  - 附件扫描、PDF 转图、Lsky 上传、回写 memo 内容
- `server/router/api/v1/memo_export_metadata_service.go`
  - 查询单条 memo 的导出元数据
- `server/router/api/v1/memo_export_metadata_helper.go`
  - memo 变更后同步 `memo_export.updated_ts`
- `server/router/api/v1/v1.go`
  - 自定义 HTTP 路由注册

### 2.2 Store / DB

- `store/memo_export.go`
  - `MemoExport` 模型与 Store 封装
- `store/driver.go`
  - Driver 接口扩展
- `store/db/sqlite/memo_export.go`
- `store/db/mysql/memo_export.go`
- `store/db/postgres/memo_export.go`
  - 三种数据库驱动实现

### 2.3 Migration

- `store/migration/sqlite/0.27/05__memo_export.sql`
- `store/migration/mysql/0.27/05__memo_export.sql`
- `store/migration/postgres/0.27/05__memo_export.sql`
- `store/migration/sqlite/LATEST.sql`
- `store/migration/mysql/LATEST.sql`
- `store/migration/postgres/LATEST.sql`

### 2.4 前端

- `web/src/components/Settings/PreferencesSection.tsx`
  - 设置页入口
- `web/src/hooks/useMemoExportMetadata.ts`
  - 读取导出时间元数据
- `web/src/components/MemoView/components/MemoHeader.tsx`
  - 时间栏后追加显示 `export_time`
- `web/src/locales/en.json`
- `web/src/locales/zh-Hans.json`

### 2.5 测试

- `server/router/api/v1/memo_service_export_test.go`
- `server/router/api/v1/memo_service_lsky_sync_test.go`

## 3. Memo 导出功能说明

### 3.1 导出范围

当前逻辑通过 `listExportableMemos()` 获取：

- 当前登录用户
- `NORMAL` + `ARCHIVED`
- 排除 comments

排序规则：

- 默认按 memo 展示时间排序
- 若实例设置 `DisplayWithUpdateTime=true`，则按 `updated_ts`
- 否则按 `created_ts`

### 3.2 文件命名规则

格式：

```text
yyyy-mm-dd-<normalized-title>-<memo-uid>.md
```

规则：

- 日期来自 memo 展示时间
- 优先使用 H1 标题作为 title
- 若无 title，则取 content snippet 的前 16 个规格化字符
- 若规格化后为空，则回退为 `memo`

### 3.3 文件内容格式

导出内容使用 Jekyll front matter：

```md
---
layout: post
title: <title>
date: <yyyy-mm-dd>
description: <摘要>
categories: <第一个 tag，若无则空>
tags:
  - ...
visibility: private
comments: false
---

<memo content>
```

### 3.4 路由与请求

路由：

- `POST /api/v1/memos/export-markdown`

请求体：

```json
{
  "outputDirectory": "exports/posts"
}
```

说明：

- 相对路径会基于 `Profile.Data` 解析
- 为防止越界，相对路径必须仍位于数据目录内
- 绝对路径允许直接使用

### 3.5 导出时间元数据

导出成功后会写入 `memo_export` 表：

- `memo_id`
- `export_ts`
- `created_ts`
- `updated_ts`

语义：

- `export_ts`：该 memo 最近一次成功导出的时间
- `created_ts`：这条导出元数据记录首次建立时间
- `updated_ts`：该 memo 最近一次内容修改时间（若存在导出记录）

重要说明：

- `updated_ts` 不是“最近一次导出的时间”
- 最近一次导出时间看 `export_ts`
- `updated_ts` 用来表示“memo 导出之后，后续这条 memo 最近又被改过的时间”

### 3.6 导出元数据查询

路由：

- `GET /api/v1/memos/:memo/export-metadata`

返回示例：

```json
{
  "exportTs": 1774588800
}
```

前端仅用它来决定是否展示 `export_time`。

## 4. memo_export 表设计说明

### 4.1 表结构用途

新增独立表而不修改 `memo` 主表，原因是：

- 避免污染主业务表
- 避免为了一个增强字段大幅侵入 proto/主模型
- 更适合做可选增强功能
- 后续如果要扩展导出状态、导出目标、导出 hash，也容易追加

### 4.2 字段语义

- `memo_id`
  - 对应 memo 主键
- `export_ts`
  - 最近一次成功导出 Markdown 的时间
- `created_ts`
  - 导出元数据首次建立时间
- `updated_ts`
  - memo 最近一次更新时同步到此记录的时间，仅在该 memo 已有导出记录时更新

### 4.3 当前更新规则

1. 首次导出时：
   - 插入一条 `memo_export`
   - `export_ts` 写当前导出时间
   - `created_ts` / `updated_ts` 由库默认生成

2. 再次导出时：
   - `upsert` 覆盖 `export_ts`
   - `updated_ts` 同时刷新

3. 之后用户修改该 memo 时：
   - 若该 memo 已有导出记录
   - 则把 `memo_export.updated_ts` 同步为 memo 的最新 `updated_ts`

当前已接入同步的入口：

- 标准 `UpdateMemo`
- Lsky 同步回写 memo 内容

如果后续新增其他“直接修改 memo 内容”的入口，也必须记得调用：

- `syncMemoExportUpdatedTs(ctx, memo.ID, memo.UpdatedTs)`

## 5. 兰空附件同步功能说明

### 5.1 处理规则

扫描当前用户全部可导出 memo 后：

- 无附件：跳过
- 附件是图片：上传到 Lsky
- 附件是 PDF：尝试转图后上传
- 附件是压缩包或不可转图文件：整条 memo 跳过

### 5.2 路由与请求

路由：

- `POST /api/v1/memos/sync-attachments-to-lsky`

请求体：

```json
{
  "baseUrl": "https://lsky.wodedata.com/api/v1",
  "token": "<lsky token>",
  "strategyId": "<optional>"
}
```

### 5.3 Lsky 上传

当前使用：

- `POST {baseUrl}/upload`

请求头：

- `Authorization: Bearer <token>`
- `Accept: application/json`

表单字段：

- `file`
- `strategy_id`，仅在前端提供时传

返回值读取：

- `data.links.url`
- 若 URL 为空则使用 `data.links.markdown` 兜底解析

### 5.4 PDF 转图策略

当前优先级：

1. `magick`
2. macOS 下回退 `qlmanage`

若两者都不可用：

- PDF memo 直接跳过

注意：

- 这是强依赖本机工具的实现
- 如果未来要跨平台长期维护，建议改为统一引入稳定的 PDF 渲染方案

### 5.5 防重复写入

追加到 memo 内容末尾的区域使用标记：

```text
<!-- memos-lsky-sync:start -->
<!-- memos-lsky-sync:end -->
```

若 memo 正文已包含该标记，当前逻辑会认为已同步过，不再重复追加。

## 6. 前端设置页实现说明

文件：

- `web/src/components/Settings/PreferencesSection.tsx`

当前设置页包含两个独立模块：

1. Memo 导出
2. 兰空附件同步

### 6.1 必须保持独立

这两个功能是独立功能，不能互相 block。

已知踩坑：

- 早期自定义路由使用了带 `:` 的路径
  - `/api/v1/memos:export`
  - `/api/v1/memos:sync-attachments-to-lsky`
- 对 Echo 来说，`:` 会参与路径参数解析，存在错误匹配风险
- 曾出现点击“导出 Markdown”却命中 Lsky 同步逻辑并报 `lsky token is required`

现已修正为：

- `/api/v1/memos/export-markdown`
- `/api/v1/memos/sync-attachments-to-lsky`

前端按钮也显式加了：

- `type="button"`

升级时务必保留这两个原则：

1. 两个动作使用不同 handler
2. 两个按钮都不要依赖表单 submit 默认行为

## 7. 与主仓库升级兼容时的重点检查项

后续如果上游主仓库做了较大改动，优先检查以下内容：

### 7.1 API 层

- `server/router/api/v1/v1.go` 是否还允许注册自定义 HTTP 路由
- Echo / Gateway 初始化方式是否改变
- 自定义接口是否被新的统一路由封装替代

### 7.2 Memo 模型

- `store.Memo`
- `memo payload`
- `memopayload.RebuildMemoPayload()`

如果上游改了 memo payload 结构或生成逻辑，要确认：

- 导出 title / tags 提取是否仍然可用
- Lsky 回写后 payload 是否仍能正确重建

### 7.3 前端设置页

- `PreferencesSection.tsx` 是否被拆分
- 设置页组件路径是否变化
- 自定义 fetch 是否应迁移到统一 hooks / connect client

### 7.4 路由层

如果上游转回严格 proto 驱动：

- 可以考虑把“导出 Markdown”和“导出元数据查询”迁移为正式 proto RPC
- 当前环境缺少 `buf` 时使用了自定义 HTTP 路由，这是一个现实取舍

### 7.5 数据库

需要确认 `memo_export` 相关内容是否仍保留：

- 新 migration 文件
- 三套 `LATEST.sql`
- 三套 driver 实现
- `store/driver.go` 接口

如果只迁移了一部分，很容易出现：

- 新库可启动但旧库升级失败
- 某一种数据库驱动编译失败

## 8. 如果需要重新移植这两个功能，建议顺序

1. 先移植 `memo_export` 表与 store 层
2. 再恢复 Memo 导出接口与测试
3. 再恢复导出时间前端展示
4. 再恢复 Lsky 同步接口
5. 最后恢复设置页入口与文案

原因：

- Memo 导出依赖更少，适合作为第一阶段恢复
- Lsky 同步依赖附件、文件读取、PDF 转换与外部 API，复杂度更高

## 9. 回归验证清单

### 9.1 Memo 导出

1. 设置页填写导出目录并点击导出
2. 确认生成 Markdown 文件
3. 确认 front matter 格式正确
4. 确认 `memo_export` 有记录
5. 回到 memo 页面确认出现 `export_time`

### 9.2 memo_export.updated_ts

1. 先导出一条 memo
2. 修改这条 memo 内容
3. 确认 `memo.updated_ts` 变化
4. 确认 `memo_export.updated_ts` 同步变化
5. 确认 `memo_export.export_ts` 不因普通编辑而变化

### 9.3 兰空附件同步

1. 使用真实 token
2. 扫描结果应优先显示有附件的 memo
3. 图片附件应能上传并回写链接
4. PDF 附件在本机工具可用时应能转图上传
5. ZIP 等附件应显示 skipped
6. 再次执行不应重复追加同一批链接

## 10. 当前已知局限

1. 自定义接口未进入 proto / Connect 正式定义
   - 原因是当前环境缺少 `buf`
   - 若未来工具链齐全，建议收编成 proto RPC

2. `export_time` 前端展示目前通过额外 HTTP 查询获取
   - 没直接塞进主 Memo proto
   - 好处是侵入性小
   - 代价是多一次请求

3. PDF 转图依赖本地工具
   - `magick`
   - macOS `qlmanage`

4. Lsky 同步只处理 attachment 记录
   - memo 正文里本来就有的外链图片不算附件

## 11. 推荐后续优化

1. 将导出接口和导出元数据接口收编到 proto
2. 将 `export_ts` / `export_updated_ts` 正式并入 Memo API 输出
3. 为 Lsky 同步增加 dry-run 模式
4. 为 Lsky 同步增加“仅处理最近 N 天修改的 memo”
5. 为导出内容增加可配置 front matter 模板

