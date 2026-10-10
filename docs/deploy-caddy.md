# 反向代理：Caddy 与 nginx

OpenChat 的二进制已经自带前端，所以前面只需要一层反向代理来终结 TLS。用 `deploy/docker-compose.yml` 部署时这一层已经配好（`deploy/Caddyfile`），这里给的是自己维护代理时的写法。

推荐 Caddy：证书自动申请和续期，默认开 HTTP/2 和 HTTP/3。在高延迟、丢包多的线路上这很要紧。

## 必须做对的一件事：来访地址

登录、注册和游客模式都按来访 IP 限流。服务端读来访地址的顺序是 `True-Client-IP`、`X-Real-IP`、`X-Forwarded-For` 第一项——这些都是请求头，**访客自己也能发**。所以代理必须：

- 删掉访客发来的 `True-Client-IP`；
- 用它自己看到的连接地址**覆盖** `X-Real-IP`（不是追加）；
- `X-Forwarded-For` 也只放连接地址，或者确保它不信任访客带来的值。

否则任何人随手填一个地址就能绕过这些限制。前面还有 CDN 时，要按 CDN 的说明取真实地址，再按上面的规则交给 OpenChat。

## Caddyfile

```caddyfile
example.com {
	encode zstd gzip
	reverse_proxy 127.0.0.1:8080 {
		header_up -True-Client-IP
		header_up X-Real-IP {remote_host}
		# 回答是流式的（SSE），不能缓冲
		flush_interval -1
	}
}
```

Caddy 不信任访客带来的 `X-Forwarded-For`，会自己写入连接地址。`encode` 不会影响流式响应（服务端给它们带了 `no-transform`），也不会重复压缩服务端已经压好的静态文件。

## nginx

```nginx
server {
    listen 443 ssl http2;
    server_name example.com;
    # ssl_certificate / ssl_certificate_key 自行配置

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header True-Client-IP "";
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $remote_addr;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    # 发消息、事件流、游客的回答：不能缓冲，也不能让读超时掐掉长连接
    location ~ ^/api/v1/(conversations/[^/]+/(events|messages|regenerate)|guest/runs)$ {
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

服务端对流式响应会发 `X-Accel-Buffering: no`，nginx 看到它也会关掉缓冲；上面显式写 `proxy_buffering off` 是双保险。
