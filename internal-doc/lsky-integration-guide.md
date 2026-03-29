# 兰空图床（Lsky）API 接入与图片迁移使用指南

本文档基于实际接入兰空图床（Lsky）的经验整理，目标是让其他项目、一次性迁移脚本、定时任务或批处理工具可以按同一套方式稳定接入 Lsky。

关联资料：

- [Lsky 2.1 API 文档](./lsky-2.1-api-doc.md)

## 1. 适用场景

这份指南适合以下场景：

- 把历史图片批量迁移到 Lsky
- 在博客、知识库、笔记系统中把本地附件上传到 Lsky
- 在自动化脚本中把 PDF 预览图或截图上传到 Lsky
- 为已有 Markdown 内容补齐图床 URL

## 2. 推荐的基础接入方式

推荐采用下面这套最小且稳定的接入方式：

- API Base URL 默认使用：`https://lsky.wodedata.com/api/v1`
- 上传接口使用：`POST /upload`
- 认证方式使用：`Authorization: Bearer <token>`
- `Accept` 固定设置为：`application/json`
- 上传体使用：`multipart/form-data`
- 必传字段：`file`
- 可选字段：`strategy_id`

上传成功后，建议优先读取：

- `data.links.url`

如果你接入其他脚本工具，也建议优先把这个字段当成最终图片 URL。

## 3. 认证方式

### 3.1 获取 Token

Lsky 2.1 支持先用账号密码换取 token，再用 Bearer Token 调用接口。

示例：

```bash
curl --location 'https://lsky.wodedata.com/api/v1/tokens' \
  --header 'Content-Type: application/x-www-form-urlencoded' \
  --data-urlencode 'email=<your-email>' \
  --data-urlencode 'password=<your-password>'
```

返回中取：

```json
{
  "status": true,
  "message": "success",
  "data": {
    "token": "xxxx"
  }
}
```

后续请求统一带：

```http
Authorization: Bearer <token>
Accept: application/json
```

### 3.2 生产环境建议

不要在脚本里硬编码账号密码。建议：

- 预先手动生成 token
- token 通过环境变量传入
- CI / 定时任务里使用 secret 管理

推荐命名：

```bash
LSKY_BASE_URL=https://lsky.wodedata.com/api/v1
LSKY_TOKEN=xxxx
LSKY_STRATEGY_ID=1
```

## 4. 最小可用上传请求

### 4.1 cURL 示例

```bash
curl --location 'https://lsky.wodedata.com/api/v1/upload' \
  --header 'Authorization: Bearer <token>' \
  --header 'Accept: application/json' \
  --form 'file=@"/path/to/example.png"' \
  --form 'strategy_id="1"'
```

如果不需要指定策略，可以省略 `strategy_id`。

### 4.2 返回值里重点关注的字段

上传响应里，建议重点解析：

- `status`
- `message`
- `data.links.url`
- `data.key`
- `data.name`
- `data.pathname`
- `data.md5`
- `data.sha1`

建议最少依赖：

- `status`
- `message`
- `data.links.url`

最小判断逻辑：

1. HTTP 状态码必须是 `200`
2. JSON 里的 `status` 必须是 `true`
3. `data.links.url` 必须非空

任一条件不满足，都应视为上传失败。

## 5. 推荐的通用接入流程

建议把迁移任务分成 5 步：

1. 枚举待迁移文件
2. 判断文件类型
3. 生成可上传的二进制内容
4. 调用 Lsky `/upload`
5. 回写目标系统中的图片 URL

### 5.1 文件类型建议

推荐按以下规则处理：

- 图片：直接上传
- PDF：先转成图片，再上传
- ZIP / RAR / 7z / 其他二进制附件：跳过
- 无法判断类型：跳过并记录日志

## 6. 图片与 PDF 的处理经验

### 6.1 图片

图片可直接上传，建议保留原文件名，便于排查和人工校验。

### 6.2 PDF

实践里更推荐的方式是：

- 先将 PDF 转为预览图
- 再把预览图上传到 Lsky

这样做的原因：

- 图床主要面向图片访问
- Markdown / 博客系统更容易直接展示图片
- 目标系统通常并不需要一个 PDF 二进制下载地址，而更需要预览图

PDF 转图时可优先尝试：

1. `ImageMagick`
2. macOS 下回退 `qlmanage`

如果你的脚本工具也要支持 PDF，建议明确区分：

- “上传原 PDF”
- “上传 PDF 预览图”

通常迁移图文内容时，后者更实用。

## 7. 幂等与去重建议

批量迁移时，最常见的问题不是“上传失败”，而是“重复上传”。

推荐至少选择一种幂等策略：

### 7.1 目标系统标记

例如在回写内容里插入一段同步标记：

```html
<!-- lsky-sync:start -->
<!-- lsky-sync:end -->
```

下次看到这个标记，就跳过。

### 7.2 本地状态表

在脚本侧维护一个 SQLite / JSONL / CSV 状态文件，记录：

- 源文件路径
- 源记录 ID
- 上传时间
- Lsky URL
- Lsky key

### 7.3 内容哈希

如果源系统里能拿到 `md5` 或 `sha1`，建议记录：

- 源文件哈希
- Lsky 返回的 `md5` / `sha1`

这样后续迁移或补偿重试时更容易避免重复。

## 8. 错误处理建议

### 8.1 必须记录的失败上下文

每次上传失败，建议至少记录：

- 源记录 ID
- 源文件名
- MIME 类型
- 文件大小
- HTTP 状态码
- Lsky `message`
- 重试次数

### 8.2 常见失败类型

- `401`：token 无效或过期
- `403`：接口被禁用或权限不足
- `429`：频率限制
- `500`：Lsky 服务端异常
- JSON 成功但 `data.links.url` 为空：按失败处理

### 8.3 重试策略

建议：

- `429`、网络超时、临时 `5xx`：可重试
- `401`、参数错误、文件类型错误：不要重试

推荐指数退避：

- 第 1 次重试：2 秒
- 第 2 次重试：5 秒
- 第 3 次重试：10 秒

超过阈值后写入失败清单，后续人工补偿。

## 9. 推荐的上传函数契约

无论你用 Go、Python 还是 Shell，建议都抽象出统一函数：

```text
upload(file_bytes, filename, token, base_url, strategy_id?) -> { url, key, md5, sha1 }
```

最少返回：

- `url`
- `key`

这样后续回写 Markdown、数据库、或其他系统时更统一。

## 10. Python 接入示例

下面这个 Python 示例尽量保持精简，适合直接拷到其他项目里：

```python
import os
import requests


def upload_image_to_lsky(file_path: str, token: str, base_url: str, strategy_id: str | None = None) -> dict:
    url = base_url.rstrip("/") + "/upload"
    headers = {
        "Authorization": f"Bearer {token}",
        "Accept": "application/json",
    }

    data = {}
    if strategy_id:
        data["strategy_id"] = str(strategy_id)

    with open(file_path, "rb") as f:
        files = {
            "file": (os.path.basename(file_path), f),
        }
        resp = requests.post(url, headers=headers, data=data, files=files, timeout=120)

    resp.raise_for_status()
    result = resp.json()

    if not result.get("status"):
        raise RuntimeError(f"lsky upload failed: {result.get('message', 'unknown error')}")

    image_url = result.get("data", {}).get("links", {}).get("url")
    if not image_url:
        raise RuntimeError("lsky upload response did not include data.links.url")

    return {
        "url": image_url,
        "key": result.get("data", {}).get("key"),
        "md5": result.get("data", {}).get("md5"),
        "sha1": result.get("data", {}).get("sha1"),
        "raw": result,
    }
```

如果你要做批量迁移，建议再包一层：

- 文件类型判断
- 重试
- 状态记录
- 回写目标系统

## 11. Shell 脚本接入建议

如果你用 Shell 批量迁移，建议：

- 文件枚举用 `find`
- MIME 判断用 `file --mime-type`
- JSON 解析用 `jq`
- 失败记录落到 `.jsonl`

下面是一个更完整一点的 Shell 示例，包含：

- 图片类型判断
- 上传
- 提取最终 URL
- 记录失败

```bash
#!/usr/bin/env bash
set -euo pipefail

: "${LSKY_BASE_URL:?LSKY_BASE_URL is required}"
: "${LSKY_TOKEN:?LSKY_TOKEN is required}"

upload_file() {
  local file="$1"
  local response

  if [[ -n "${LSKY_STRATEGY_ID:-}" ]]; then
    response="$(curl --silent --show-error --fail \
      --location "${LSKY_BASE_URL%/}/upload" \
      --header "Authorization: Bearer ${LSKY_TOKEN}" \
      --header "Accept: application/json" \
      --form "file=@${file}" \
      --form "strategy_id=${LSKY_STRATEGY_ID}")"
  else
    response="$(curl --silent --show-error --fail \
      --location "${LSKY_BASE_URL%/}/upload" \
      --header "Authorization: Bearer ${LSKY_TOKEN}" \
      --header "Accept: application/json" \
      --form "file=@${file}")"
  fi

  local status
  local url
  status="$(printf '%s' "$response" | jq -r '.status')"
  url="$(printf '%s' "$response" | jq -r '.data.links.url // empty')"

  if [[ "$status" != "true" || -z "$url" ]]; then
    echo "upload failed for ${file}: ${response}" >&2
    return 1
  fi

  printf '%s\n' "$url"
}
```

```bash
find ./images -type f | while read -r file; do
  mime="$(file --brief --mime-type "$file")"
  case "$mime" in
    image/*)
      url="$(upload_file "$file")" || {
        printf '{"file":"%s","error":"upload failed"}\n' "$file" >> failed.jsonl
        continue
      }
      printf '%s -> %s\n' "$file" "$url"
      ;;
    application/pdf)
      echo "skip pdf for now: $file"
      ;;
    *)
      echo "skip unsupported file: $file"
      ;;
  esac
done
```

如果要支持 PDF 转图，Shell 脚本建议把“转图”单独拆成一步，不要和上传过程混在一起。

## 12. Python 批量迁移完整脚本示例

下面给一个更完整的 Python 批量迁移脚本模板，适合：

- 扫描目录下所有图片
- 上传到 Lsky
- 记录成功结果到 `uploaded.jsonl`
- 记录失败结果到 `failed.jsonl`

```python
#!/usr/bin/env python3
import json
import mimetypes
import os
import time
from pathlib import Path

import requests


LSKY_BASE_URL = os.environ["LSKY_BASE_URL"]
LSKY_TOKEN = os.environ["LSKY_TOKEN"]
LSKY_STRATEGY_ID = os.environ.get("LSKY_STRATEGY_ID")
SOURCE_DIR = Path(os.environ.get("SOURCE_DIR", "./images"))


def upload_image_to_lsky(file_path: Path) -> dict:
    url = LSKY_BASE_URL.rstrip("/") + "/upload"
    headers = {
        "Authorization": f"Bearer {LSKY_TOKEN}",
        "Accept": "application/json",
    }
    data = {}
    if LSKY_STRATEGY_ID:
        data["strategy_id"] = LSKY_STRATEGY_ID

    with file_path.open("rb") as f:
        files = {"file": (file_path.name, f)}
        resp = requests.post(url, headers=headers, data=data, files=files, timeout=120)

    resp.raise_for_status()
    result = resp.json()

    if not result.get("status"):
        raise RuntimeError(result.get("message", "lsky upload failed"))

    image_url = result.get("data", {}).get("links", {}).get("url")
    if not image_url:
        raise RuntimeError("missing data.links.url in lsky response")

    return {
        "file": str(file_path),
        "url": image_url,
        "key": result.get("data", {}).get("key"),
        "md5": result.get("data", {}).get("md5"),
        "sha1": result.get("data", {}).get("sha1"),
    }


def is_uploadable_image(file_path: Path) -> bool:
    mime, _ = mimetypes.guess_type(str(file_path))
    return bool(mime and mime.startswith("image/"))


def append_jsonl(path: str, payload: dict) -> None:
    with open(path, "a", encoding="utf-8") as f:
        f.write(json.dumps(payload, ensure_ascii=False) + "\n")


def main() -> None:
    for file_path in SOURCE_DIR.rglob("*"):
        if not file_path.is_file():
            continue
        if not is_uploadable_image(file_path):
            continue

        try:
            result = upload_image_to_lsky(file_path)
            append_jsonl("uploaded.jsonl", result)
            print(f"uploaded: {file_path} -> {result['url']}")
            time.sleep(0.2)
        except Exception as exc:
            append_jsonl("failed.jsonl", {"file": str(file_path), "error": str(exc)})
            print(f"failed: {file_path} -> {exc}")


if __name__ == "__main__":
    main()
```

运行方式：

```bash
export LSKY_BASE_URL="https://lsky.wodedata.com/api/v1"
export LSKY_TOKEN="your-token"
export LSKY_STRATEGY_ID="1"
export SOURCE_DIR="./images"
python3 migrate_lsky.py
```

如果你想做断点续传，建议先把 `uploaded.jsonl` 读入内存，按 `file` 字段跳过已成功文件。

## 13. Shell + ImageMagick 处理 PDF 转图示例

如果迁移源里包含 PDF，并且目标只是要在 Markdown 或博客里展示预览图，推荐先把 PDF 转成图片，再上传。

下面这个示例依赖 `ImageMagick` 的 `magick` 命令：

```bash
#!/usr/bin/env bash
set -euo pipefail

pdf_file="$1"
output_dir="${2:-./pdf-preview}"

mkdir -p "$output_dir"

# 只导出第一页为 png；如果你想导出全部页面，可以去掉 [0]
magick -density 200 "${pdf_file}[0]" -quality 92 "${output_dir}/$(basename "${pdf_file%.pdf}").png"
```

如果要把“PDF 转图 + 上传”串起来，可以这样做：

```bash
pdf="./docs/example.pdf"
tmp_dir="$(mktemp -d)"
preview_png="${tmp_dir}/preview.png"

magick -density 200 "${pdf}[0]" -quality 92 "${preview_png}"

curl --silent --show-error --fail \
  --location "${LSKY_BASE_URL%/}/upload" \
  --header "Authorization: Bearer ${LSKY_TOKEN}" \
  --header "Accept: application/json" \
  --form "file=@${preview_png}" \
  | jq -r '.data.links.url'
```

注意：

- 大批量 PDF 转图时要关注 CPU 和磁盘占用
- 若 PDF 很大，建议只转第一页
- 若你需要更高质量缩略图，可调高 `-density`
- 若脚本会长期运行，记得删除临时目录

## 14. Markdown 文本中批量替换图片地址到 Lsky 的专项示例

适合场景：

- 你已经把图片先迁到了 Lsky
- 手上有一份“旧地址 -> 新地址”的映射表
- 想批量替换 Markdown 文本中的图片链接

假设你有一个 `mapping.json`：

```json
{
  "https://old.example.com/a.png": "https://lsky.example.com/i/2026/03/a.png",
  "https://old.example.com/b.jpg": "https://lsky.example.com/i/2026/03/b.jpg"
}
```

下面这个 Python 脚本会批量替换 Markdown 文件中的图片 URL：

```python
#!/usr/bin/env python3
import json
import re
from pathlib import Path


MARKDOWN_DIR = Path("./posts")
MAPPING_FILE = Path("./mapping.json")


with MAPPING_FILE.open("r", encoding="utf-8") as f:
    mapping = json.load(f)


image_pattern = re.compile(r'!\[([^\]]*)\]\(([^)]+)\)')


def replace_markdown_images(text: str) -> str:
    def repl(match: re.Match) -> str:
        alt = match.group(1)
        url = match.group(2)
        new_url = mapping.get(url, url)
        return f"![{alt}]({new_url})"

    return image_pattern.sub(repl, text)


for md_file in MARKDOWN_DIR.rglob("*.md"):
    original = md_file.read_text(encoding="utf-8")
    updated = replace_markdown_images(original)
    if updated != original:
        md_file.write_text(updated, encoding="utf-8")
        print(f"updated: {md_file}")
```

如果你的 Markdown 里还有这种 HTML 图片：

```html
<img src="https://old.example.com/a.png" />
```

那可以额外再补一层 HTML 替换逻辑。最稳妥的做法是：

1. 先处理 Markdown 图片语法
2. 再处理 HTML `<img>`
3. 最后输出替换报告

## 15. 与 Markdown / 博客系统集成建议

若目标系统是 Markdown，建议上传后统一生成：

```md
![alt](https://your-lsky-url/example.png)
```

若目标系统是博客 / CMS，建议额外保存：

- 原始文件名
- Lsky URL
- Lsky key
- 上传时间

这样后续更方便重建内容或做删除同步。

## 16. 迁移任务的推荐顺序

推荐按下面顺序实施：

1. 先做小样本测试
2. 确认 token / strategy_id / URL 都正确
3. 只迁移图片
4. 再加入 PDF 转图
5. 最后再做批量回写

不要一开始就“边扫全量数据边直接覆盖原内容”。

## 17. 总结出的最佳实践

推荐在其他项目里也坚持以下原则：

- 上传接口只封装一层，保持简单
- 永远显式设置 `Accept: application/json`
- 永远检查 `status` 和 `data.links.url`
- PDF 和图片分开处理
- 不支持的附件直接跳过，不做半成功写入
- 回写目标系统前先完成全部上传
- 用标记或状态表保证幂等
- 日志里保留足够的失败上下文

## 18. 后续可扩展方向

如果后面要做更通用的图片迁移工具，建议继续扩展：

- 上传前按哈希去重
- 支持按相册归档
- 支持删除同步
- 支持 dry-run
- 支持并发上传与速率限制
- 支持迁移报告导出

## 19. 推荐的落地方式

如果你要把这份指南用于其他项目，建议优先选下面两种方式之一：

- 小规模迁移：直接用 Shell + `curl` + `jq`
- 中大规模迁移：用 Python 封装上传函数、状态文件和重试逻辑

如果后续还要反复使用，建议单独沉淀成一个 CLI 工具，而不是把上传逻辑散落在各个项目里。
