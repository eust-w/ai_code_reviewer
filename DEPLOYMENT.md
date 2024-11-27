# AI 代码审查机器人部署手册

本文档提供了 AI 代码审查机器人的详细部署指南，适用于 GitHub、GitLab 和 Gitea 平台。

## 目录

- [前提条件](#前提条件)
- [构建应用](#构建应用)
- [配置文件](#配置文件)
- [GitHub 部署](#github-部署)
- [GitLab 部署](#gitlab-部署)
- [Gitea 部署](#gitea-部署)
- [服务器部署](#服务器部署)
- [故障排除](#故障排除)

## 前提条件

- Go 1.18 或更高版本（仅用于构建）
- 一台可公网访问的 Linux 服务器
- Git 平台（GitHub、GitLab 或 Gitea）的管理员权限
- LLM API 访问权限（OpenAI、Claude 或 Deepseek）

## 构建应用

### 获取源码

```bash
git clone https://github.com/eust-w/ai_code_reviewer.git
cd ai_code_reviewer
```

### 编译二进制文件

```bash
# 编译 Linux 版本
make build-linux-amd64

# 或者直接使用 Go 命令
GOOS=linux GOARCH=amd64 go build -o bin/cr-bot-linux-amd64 ./cmd/server
```

## 配置文件

创建 `.env` 文件，根据您的部署平台和 LLM 选择进行配置：

```env
# 平台选择
# 选项: github, gitlab, gitea
PLATFORM=github

# 通用配置
WEBHOOK_SECRET=your-secure-webhook-secret
# 如果设置，只有带有此标签的 PR 才会被审查
TARGET_LABEL=needs-review

# GitHub 配置
GITHUB_TOKEN=your_github_token

# GitLab 配置
# GITLAB_TOKEN=your_gitlab_token
# GITLAB_BASE_URL=https://gitlab.com/api/v4

# Gitea 配置
# GITEA_TOKEN=your_gitea_token
# GITEA_BASE_URL=https://your-gitea-instance.com/api/v1

# 服务器配置
PORT=8008
LOG_LEVEL=debug

# LLM 配置选项

# 选项 1: LLM 代理配置 (推荐)
LLM_PROXY_ENDPOINT=https://your-llm-proxy-endpoint/v1/chat/completions
LLM_PROXY_API_KEY=your-llm-proxy-api-key

# 模型配置
CLAUDE_MODEL_NAME=aws/claude-3-5-sonnet
CLAUDE_MAX_TOKENS=4000

# 或者使用 Deepseek 模型
DEEPSEEK_MODEL_NAME=bce/deepseek-v3

# 选项 2: 直接 LLM 提供商配置
# DIRECT_LLM_ENDPOINT=https://api.provider.com/v1/chat/completions
# DIRECT_LLM_MODEL_ID=model-id
# DIRECT_LLM_API_KEY=your-api-key
# DIRECT_LLM_PROVIDER_TYPE=provider-name

# 选项 3: OpenAI 配置
# OPENAI_API_KEY=your-openai-api-key
# OPENAI_API_ENDPOINT=https://api.openai.com/v1
# MODEL=gpt-4o

# 选项 4: Azure OpenAI 配置
# OPENAI_API_KEY=your-azure-openai-api-key
# OPENAI_API_ENDPOINT=https://your-resource.openai.azure.com
# AZURE_API_VERSION=2023-05-15
# AZURE_DEPLOYMENT=your-deployment-name
# MODEL=gpt-4

# 其他配置
LANGUAGE=English  # 或 Chinese
PROMPT=Please review the following code patch. Focus on potential bugs, risks, and improvement suggestions.
MAX_PATCH_LENGTH=10000

# 文件过滤配置
IGNORE_PATTERNS=/node_modules/**/*,/vendor/**/*
INCLUDE_PATTERNS=*
```

## GitHub 部署

### 创建 GitHub Token

1. 登录 GitHub 账号
2. 访问 Settings > Developer settings > Personal access tokens > Tokens (classic)
3. 点击 "Generate new token"
4. 为 token 设置描述，如 "AI Code Reviewer"
5. 选择以下权限：
   - `repo` - 完整的仓库访问权限
   - `admin:repo_hook` - 仓库 webhook 管理权限
6. 生成 token 并复制保存，填入 `.env` 文件的 `GITHUB_TOKEN` 字段

### 配置 Webhook

#### 组织级别 Webhook

1. 访问组织设置页面：`https://github.com/organizations/[组织名称]/settings/hooks`
2. 点击 "Add webhook"
3. 配置 Webhook：
   - Payload URL: `https://[您的服务器域名]:[端口]/webhook`
   - Content type: `application/json`
   - Secret: 填入与 `.env` 文件中 `WEBHOOK_SECRET` 相同的值
   - 选择 "Let me select individual events"，然后勾选 "Pull requests"
   - 确保 "Active" 选项被勾选
4. 点击 "Add webhook" 保存

#### 单个仓库 Webhook

1. 访问仓库设置页面：`https://github.com/[用户名或组织名]/[仓库名]/settings/hooks`
2. 按照上述组织级别的相同步骤配置 Webhook

## GitLab 部署

### 创建 GitLab Token

1. 登录 GitLab 账号
2. 访问 User Settings > Access Tokens
3. 创建一个新 token：
   - 名称：AI Code Reviewer
   - 范围：api, read_repository, write_repository
4. 点击 "Create personal access token"
5. 复制生成的 token 并填入 `.env` 文件的 `GITLAB_TOKEN` 字段

### 配置 Webhook

#### 组级别 Webhook

1. 访问组设置页面：`https://gitlab.com/groups/[组名]/-/settings/integrations`
2. 添加新的 Webhook：
   - URL: `https://[您的服务器域名]:[端口]/webhook`
   - Secret Token: 填入与 `.env` 文件中 `WEBHOOK_SECRET` 相同的值
   - 勾选 "Merge request events"
   - 确保 "Enable SSL verification" 被勾选（如果您的服务器支持 HTTPS）
3. 点击 "Add webhook"

#### 项目级别 Webhook

1. 访问项目设置页面：`https://gitlab.com/[用户名或组名]/[项目名]/-/settings/integrations`
2. 按照上述组级别的相同步骤配置 Webhook

## Gitea 部署

### 创建 Gitea Token

1. 登录 Gitea 账号
2. 访问 Settings > Applications > Generate New Token
3. 输入 token 描述，如 "AI Code Reviewer"
4. 点击 "Generate Token"
5. 复制生成的 token 并填入 `.env` 文件的 `GITEA_TOKEN` 字段

### 配置 Webhook

1. 访问仓库设置页面：`https://[您的Gitea实例]/[用户名]/[仓库名]/settings/hooks`
2. 点击 "Add Webhook" > "Gitea"
3. 配置 Webhook：
   - Target URL: `https://[您的服务器域名]:[端口]/webhook`
   - Secret: 填入与 `.env` 文件中 `WEBHOOK_SECRET` 相同的值
   - 勾选 "Pull Request"
   - 确保 "Active" 选项被勾选
4. 点击 "Add Webhook"

## 服务器部署

### 方法 1: 直接部署

```bash
# 创建部署目录
mkdir -p /opt/ai-code-reviewer
cd /opt/ai-code-reviewer

# 上传二进制文件和配置文件
# 将编译好的二进制文件和 .env 文件上传到此目录

# 设置执行权限
chmod +x cr-bot-linux-amd64

# 创建 systemd 服务
cat > /etc/systemd/system/ai-code-reviewer.service << EOF
[Unit]
Description=AI Code Reviewer Bot
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=/opt/ai-code-reviewer
ExecStart=/opt/ai-code-reviewer/cr-bot-linux-amd64
Restart=always
RestartSec=5
Environment=PORT=8008

[Install]
WantedBy=multi-user.target
EOF

# 启动服务
systemctl daemon-reload
systemctl enable ai-code-reviewer
systemctl start ai-code-reviewer

# 检查服务状态
systemctl status ai-code-reviewer
```

### 方法 2: Docker 部署

```bash
# 创建 Dockerfile
cat > Dockerfile << EOF
FROM alpine:latest

WORKDIR /app
COPY bin/cr-bot-linux-amd64 /app/
COPY .env /app/

RUN chmod +x /app/cr-bot-linux-amd64

EXPOSE 8008
CMD ["/app/cr-bot-linux-amd64"]
EOF

# 构建 Docker 镜像
docker build -t ai-code-reviewer:latest .

# 运行容器
docker run -d --name ai-code-reviewer \
  -p 8008:8008 \
  --restart always \
  ai-code-reviewer:latest
```

### 配置 Nginx 反向代理（推荐）

```bash
# 安装 Nginx
apt update
apt install -y nginx certbot python3-certbot-nginx

# 配置 Nginx 站点
cat > /etc/nginx/sites-available/ai-code-reviewer << EOF
server {
    listen 80;
    server_name your-domain.com;

    location / {
        proxy_pass http://localhost:8008;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
    }
}
EOF

# 启用站点配置
ln -s /etc/nginx/sites-available/ai-code-reviewer /etc/nginx/sites-enabled/
nginx -t
systemctl restart nginx

# 配置 SSL 证书
certbot --nginx -d your-domain.com
```

## 测试部署

1. 创建一个测试 Pull Request 或 Merge Request
2. 如果配置了 `TARGET_LABEL`，为 PR/MR 添加相应标签
3. 检查服务器日志：
   ```bash
   # 直接部署
   journalctl -u ai-code-reviewer -f
   
   # Docker 部署
   docker logs -f ai-code-reviewer
   ```
4. 验证 PR/MR 是否收到了代码审查评论

## 故障排除

### Webhook 未触发

- 检查 Webhook 设置页面的 "Recent Deliveries"/"Recent Events" 选项卡
- 确认服务器防火墙允许入站连接到指定端口
- 验证 Webhook URL 是否正确且可公网访问

### 审查未生成

- 检查 `.env` 文件中的 LLM 配置是否正确
- 验证 Token 是否有效且具有足够权限
- 检查服务日志中的详细错误信息

### LLM API 错误

- 验证 API 密钥是否有效
- 检查 API 端点 URL 是否正确
- 确认 API 服务是否可用，以及您的账户是否有足够的配额

### 权限问题

- 确保 Token 具有足够的权限
- 如果使用组织/组仓库，确认 Token 有权访问这些仓库

## 安全最佳实践

- 定期轮换 Token 和 Webhook Secret
- 使用 HTTPS 保护 Webhook 端点
- 限制服务器访问权限
- 考虑在隔离环境中运行服务
- 监控服务器资源使用情况
- 定期更新代码审查机器人以获取最新功能和安全修复
