# 多品牌代码实施与上线说明

本次按 V3 完成代码改造，生产部署另行处理。基线为本地
`e454927400f9d3d937401e16f12ac43c6db6feb3`，分支
`codex/multibrand-foundation`。未连接生产数据库、设置 DNS、安装生产邮件
凭据、提交 Git 或推送代码。生产 SHA、拓扑及待处理订单尚未核实。

## 实施范围

| 品牌 ID / 键 | 主域名 | 隔离关系 |
|---|---|---|
| 1 / llmp | llmp.org | llmp.cc、llmp.xyz、llmp.site 共用账户、余额、Key |
| 2 / mues | mues.cc | 独立用户、钱包、Key、身份、订单与内容 |
| 3 / aisi | aisi.plus | 独立 |
| 4 / opensi_codes | opensi.codes | 独立 |
| 5 / opensi_in | opensi.in | 独立 |

新品牌与新增域名默认停用，多品牌开关默认关闭。共享 Go 服务、
PostgreSQL、Redis 和平台 accounts；各品牌有独立 groups、费率、订阅、
营销、运营权限、文档、邮件和 OAuth 配置。

| V3 项目 | 已实现代码 |
|---|---|
| PR01–02 | migrations 269–273、internal/brand、Ent BrandMixin；历史数据回填 LLMP，保留原 ID、余额、订单号 |
| PR03 | JWT/refresh、签名 OAuth state、邮箱规范化/注册锁/验证码、密码恢复、TOTP、passkey 品牌归属 |
| PR04/07 | full/gateway 全网关别名 Host+Key+User+Group 校验；共享上游容量，品牌 RPM/并发租约；原 USD 计费 |
| PR05 | 品牌设置白名单、凭据独立；HTML/ETag、Key、仪表盘、Pelican 缓存隔离和配置失败拒绝响应 |
| PR06 | 五首页模板、主题/Logo、中文/英文、导航/页脚、CMS 栏目、发布/撤回、乐观锁版本、恢复草稿、图片预览 |
| PR08 | 商户订单快照、验签、幂等/迟到回调、退款、补单按订单品牌执行 |
| PR09 | 显式 super_admin / owner / operator / support 授权、llmp.org 平台入口、选定品牌操作与跨品牌只读总览 |
| PR10 | config.example.yaml、PowerShell 验证脚本和上线说明；生产操作尚未执行 |

报表区分钱包 USD、调用扣费、按现有账号配置计算的成本、调用毛利、
商户各币种收款/退款。该成本是现有计费估算，不能视为实际发票成本，
也不能把人民币收款与 USD 扣费直接相减。

本地 Prism 基线已实现估算计费；附件关于部分 `usage=null` 路径不计费的
描述与实际源码不同。本次保留基线的计费、模型选择、工具回合、SSE、
错误及上游调度规则，未引入另一套 Prism 计费规则。

## 权限和数据边界

Host 必须匹配已启用 domains；客户端 `X-Forwarded-Host`、
`X-Brand-ID` 不能选择品牌。站点、注册、网关分别启停。LLMP 四根域名
浏览器登录存储独立，用户分别登录同一个账户，不跨域传递 token。

Ent 和 PostgreSQL FORCE RLS 同时约束查询/写入。SQL connector 对查询、
执行、prepared statement、COPY 和事务切换受限角色/品牌变量，事务
不能换品牌。关联触发器检查 User、Group、Key、Order、Job 归属。

`sub2api_brand_runtime` 必须为 NOLOGIN / NOSUPERUSER / NOBYPASSRLS、
无表所有权，不得写 brands/domains/brand_admins/settings/security_secrets。
应用数据库登录须有该角色成员关系；迁移身份须能创建/授予角色。
多品牌启动校验角色、RLS、归属触发器，缺失时拒绝启动。

内部无品牌维护保持平台权限；异步用户任务带品牌。邮件队列、图片任务、
结算、订阅/余额提醒、用量清理及异步审计已携带归属。审计按被操作品牌
保存，共享账号和系统运维仅平台权限。

平台权限来自 brand_admins 显式授权，不仅凭 users.role。总后台从
llmp.org 登录；只有认证后的管理中间件可处理选定品牌参数。Owner 管理
本品牌邮件/运营，Operator 不能改商户、退款和运营密钥，Support 只读。

## 独立邮件与 Resend

每个品牌分别保存 `smtp_host`、`smtp_port`、`smtp_username`、
`smtp_password`、`smtp_from`、`smtp_from_name`、`smtp_use_tls`。
注册验证、密码恢复及用户通知读取该品牌配置；新品牌不会继承 LLMP
邮件或 OAuth 凭据。空密码保留已有凭据，公开配置不返回秘密。

使用项目现有 SMTP，可接 Resend 或其他 SMTP 服务。可由一个 Resend
账号管理五个发件域名，也可分别使用账号。实际 SMTP 参数、API Key、
发件域名/DNS 验证须在部署阶段按服务商控制台配置，尚未真实投递。
LLMP 四别名属于同一品牌，共用一套邮件配置。

入口 `/admin/brand-content`：先选择目标品牌，再编辑“品牌邮件发送”。

## 本地验证

需要 PowerShell 7、Docker Desktop、Node 和已安装的 frontend 依赖。
前端安装使用冻结的 pnpm 锁文件；脚本直接用 Node 运行 vue-tsc / Vite，
不调用 cmd、Bash 或修改 ExecutionPolicy。

```powershell
$repoPath = 'C:\Users\59908\Documents\ChatGPT\sub2api\sub2api-multibrand'
$scriptPath = Join-Path $repoPath 'scripts\verify-multibrand.ps1'
& $scriptPath -NodePath 'C:\Users\59908\.cache\codex-runtimes\codex-primary-runtime\dependencies\node\bin\node.exe'
```

脚本使用本任务 PostgreSQL 18 / Redis 8 容器（仅本机端口 25432/26379），
创建并清除测试数据库，Redis 使用 14/15 库，不清空全局数据。
**不要把测试 DSN 改成生产地址。** Go 在 `golang:1.27-bookworm` 中执行，
依赖/构建缓存采用命名卷。

依次执行 Vue 类型检查、品牌/XSS 测试、Vite 构建、受影响后端单元测试、
真实 PostgreSQL/Redis 多品牌集成和 embed server 构建。单元日志保存在
系统临时目录 `sub2api-multibrand-verification`。

添加 `-Preview` 启动一次性验收数据。浏览器仅访问
`llmp.localhost`、`mues.localhost`、`aisi.localhost`、
`codes.localhost`、`in.localhost`，端口均为 28080；这些域名由测试
harness 映射 Host，不进入生产白名单。登录信息由脚本输出；停止后清除
本次数据库，最长保留 25 分钟。

```powershell
Invoke-WebRequest -Uri 'http://127.0.0.1:28080/__preview/stop' -Method Post
```

集成覆盖历史升级/RLS、五品牌同邮箱注册/登录/恢复、JWT/refresh、全部
网关别名跨品牌 Key、运营权限、CMS 发布/版本/图片归属、HTML 配置热
更新、Redis RPM/SSE 租约、签名回调/部分退款/迟到回调及异步审计。
支付使用本地商户模拟器；真实邮件、OAuth 和支付需预生产验证。

## 已完成本地验收（2026-10-09）

| 检查 | 结果 |
|---|---|
| 受影响后端单元套件 | 12 个包通过，失败为 0；需外部环境的测试按其原有条件跳过 |
| 前端 | Vue 类型检查、5 条品牌/XSS 测试及 Vite 生产构建通过 |
| PostgreSQL | 历史数据升级、五品牌 HTTP 身份/Key/CMS/设置/审计隔离、签名回调与部分退款通过 |
| 财务汇总 | LLMP、MUES 各收款 70 CNY、部分退款 35 CNY，另品牌钱包与汇总不受影响 |
| Redis/Streaming | 品牌 RPM、SSE 并发租约续期、取消释放及丢失租约终止通过 |
| HTML/OAuth/Prism | 品牌 HTML/ETag 热更新、签名 OAuth state 和 Prism 离线回归通过 |
| 构建/生成 | embed server 构建通过；独立目录重新生成 Ent/Wire，与源码一致 |
| 浏览器 | 五首页品牌/地址、390px 新品牌布局、英文切换、MUES 文档和平台选定品牌 CMS 发布通过 |

一次性预览已停止、测试数据库已清除，本地测试容器保留并停止，便于复现。
Vite 存在原项目体积/浏览器数据提示，构建成功；真实上游、OAuth 服务商、
Resend 实际投递、真实商户和 CDN 行为仍属部署阶段验证。

## 生产切换

1. 核对 SHA、DB/迁移版本、full/gateway 拓扑、反代 Host、在途订单、文档
   目录，执行备份/PITR 恢复演练，记录用户数、余额和、Key/订单及对账基准。
2. 在预生产恢复历史数据并演练 269–273，验证 LLMP 回填前后数量/余额一致。
3. 停止全部旧版写入者和后台任务，再迁移并启动统一版本。270/271 已包含
   FORCE RLS 和唯一约束切换，**不能按旧新版混跑双写流程上线**。
4. 初期只开放 LLMP。启用 `multibrand.enabled` / `MULTIBRAND_ENABLED`；
   旧无品牌 JWT 只在 LLMP 的明确 RFC3339 `legacy_jwt_until` 期限内接受，
   留空表示拒绝旧 token。部署前决定重新登录或有限过渡窗口。
5. 按实际 Cloudflare/Nginx 模式提供同源 Web、`/api/v1`、`/v1` 和所有别名，
   保留验证后的 Host，分别配置 TLS/WAF；禁止无限信任公网请求头。
6. CDN 的 HTML 缓存按 Host 分区，公开 brand-config 为 no-store，身份 API
   不跨 Host 缓存；内容更新后刷新对应域名 CDN。
7. 先 llmp.cc → llmp.xyz → llmp.site，再以 mues.cc 演练注册/邮件/Key/
   模型/支付/退款/文档/权限，最后其余品牌。开放前配齐邮件、OAuth、商户、
   分组与费率，再启用品牌 status、域名 enabled 及 registration_enabled。

DNS/证书字段目前保存人工备注，不自动查询或配置供应商。跨实例/CDN
失效、真实 Resend 投递、OAuth 回跳、支付/退款、生产 SSE 和上游容量
须在部署阶段验证。本地验收不能替代这些生产检查。

## 回退

按品牌或域名停止新流量，必要时只关 public_enabled / registration_enabled，
保留既有 API 使用需求。签名有效的支付回调仍依据历史订单品牌完成对账。

激活独立品牌或产生新品牌 User/Group/Order 后，关闭整体多品牌开关会拒绝
启动。保留 schema/数据，不 down-migrate、不覆盖数据库、不恢复忽略
brand_id 的旧二进制。流量退至同样隔离的已验证版本；恢复备份前必须
处理新增钱包和在途支付，避免财务数据丢失。
