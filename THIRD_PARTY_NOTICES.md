# THIRD-PARTY NOTICES

Cadenza 在构建与分发时链接了下列第三方组件。下表与声明用于履行其各自许可证（尤其 Apache-2.0 §4）的归属与随附义务。各组件完整许可文本存放于本仓库 [`licenses/`](./licenses/) 目录；Apache License 2.0 完整文本见根目录 [`LICENSE`](./LICENSE)。

本仓库**未对任何上游源码进行修改**（opamp-go 等均以库依赖形式导入，非 fork/vendor）。

## 直接依赖

| 模块 | 版本 | 许可证 | 版权 / 归属 |
|---|---|---|---|
| `github.com/open-telemetry/opamp-go` | v0.23.0 | Apache-2.0 | The OpenTelemetry Authors |
| `trpc.group/trpc-go/trpc-agent-go` | v1.11.1 | Apache-2.0 | Tencent（见下方 NOTICE） |
| `trpc.group/trpc-go/trpc-mcp-go` | v0.0.10 | Apache-2.0 | Tencent（见下方声明头） |
| `github.com/getkin/kin-openapi` | v0.124.0 | MIT | the project authors |
| `github.com/go-sql-driver/mysql` | v1.10.0 | MPL-2.0 | The Go-MySQL-Driver Authors |
| `gopkg.in/yaml.v3` | v3.0.1 | MIT 与 Apache-2.0（双许可） | Canonical Ltd.（见下方 NOTICE） |
| `modernc.org/sqlite` | v1.56.0 | BSD-3-Clause | The Sqlite Authors |

## 必须随分发的 NOTICE 声明（Apache-2.0 §4(d)）

### trpc-agent-go — Copyright 2025 Tencent

```
tRPC-Agent-Go
Copyright (C) 2025 Tencent.

Tencent is pleased to support the open source community by making
tRPC-Agent-Go available under the Apache License, Version 2.0.
```

### gopkg.in/yaml.v3 — Copyright 2011-2016 Canonical Ltd.

```
Copyright 2011-2016 Canonical Ltd.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
```

### trpc-mcp-go — 声明头（取自其 LICENSE 文件开头）

```
Tencent is pleased to support the open source community by making trpc-mcp-go available.

Copyright (C) 2025 Tencent.  All rights reserved.

trpc-mcp-go is licensed under the Apache License Version 2.0.
```

## 间接依赖

间接依赖的完整清单及其许可证可运行 `go-licenses report ./...` 生成；各自许可文本见对应上游仓库（`go.sum` 中已锁定版本）。本表仅覆盖直接依赖及其 NOTICE 义务；如后续调整依赖树，请同步更新本文件与 [`licenses/`](./licenses/)。
