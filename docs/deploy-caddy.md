# 反向代理示例：Caddy 与 nginx

这是 [部署指南](deployment.md) 的附录，只有中文。OpenChat 的二进制已经自带前端，所以前面只需要一层反向代理来终结 TLS。用 `deploy/docker-compose.yml` 部署时这一层已经配好（`deploy/Caddyfile`），这里给的是自己维护代理时的写法。

代理要做到什么、为什么要覆盖访客自己带来的 IP 头、保留 `Host` 和 `X-Forwarded-Proto`（通行密钥要用）、前面有 CDN 怎么办，都写在部署指南的[放在别的反向代理或 CDN 后面](deployment.md#放在别的反向代理或-cdn-后面)和[按 IP 限流依赖正确的转发头](deployment.md#按-ip-限流依赖正确的转发头)里，这里不重复。下面两个例子就是照那些规则写的。

推荐 Caddy：证书自动申请和续期，默认开 HTTP/2 和 HTTP/3。在高延迟、丢包多的线路上这很要紧。

长读超时和关闭缓冲**只有事件流需要**：只有 `GET /api/v1/conversations/{id}/events` 是长连接（回答的文字从这里推给页面）。发消息等请求立刻返回 202 和一小段 JSON，用代理的默认设置就行。

## Caddyfile

```caddyfile
example.com {
	encode zstd gzip
	reverse_proxy 127.0.0.1:8080 {
		header_up -True-Client-IP
		header_up X-Real-IP {remote_host}
		# 事件流（SSE）不能缓冲
		flush_interval -1
	}
}
```

`reverse_proxy` 默认保留 `Host`、写入 `X-Forwarded-Proto`，也不信任访客带来的 `X-Forwarded-For`。`encode` 不会影响流式响应（服务端给它们带了 `no-transform`），也不会重复压缩服务端已经压好的静态文件。

## nginx

```nginx
server {
    listen 443 ssl;
    http2 on;    # nginx 1.25.1 起用这一行；旧写法 listen 443 ssl http2; 已被弃用
    server_name example.com;
    # ssl_certificate / ssl_certificate_key 自行配置

    # 本地存储驱动下，文件直接 PUT 到应用，单个最大 20 MB；nginx 默认只放行 1 MB
    client_max_body_size 25m;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header True-Client-IP "";
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $remote_addr;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    # 事件流：不能缓冲，也不能让读超时掐掉长连接
    location ~ ^/api/v1/conversations/[^/]+/events$ {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header True-Client-IP "";
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $remote_addr;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_http_version 1.1;
        proxy_set_header Connection "";
        proxy_buffering off;
        proxy_cache off;
        proxy_read_timeout 1h;
    }
}
```

两个 `location` 里的地址头要各写一遍：nginx 里只要一个 `location` 自己写了任何 `proxy_set_header`，就不再继承外层的，漏掉就等于把限流交给了访客。

服务端对事件流会发 `X-Accel-Buffering: no`，nginx 看到它也会关掉缓冲；上面显式写 `proxy_buffering off` 是双保险。
