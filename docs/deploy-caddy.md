# 用 Caddy 做反向代理

OpenChat 的二进制已经自带前端（见 README 的「部署」），所以前面只需要一层反向代理来终结 TLS。
推荐 Caddy：证书自动申请和续期，并且**默认就开 HTTP/2 和 HTTP/3**。在高延迟、丢包多的线路上这很要紧：
HTTP/2 把页面的几十个资源复用到同一条连接上，省掉逐个握手的往返；HTTP/3（QUIC）再把建连压到
更少的往返，并且不会因为一个包丢失而卡住整条连接。

## Caddyfile

```caddyfile
example.com {
	reverse_proxy localhost:8080
}
```

就这些。把 `example.com` 换成解析到这台机器的域名，80 和 443（TCP 和 UDP，HTTP/3 用 UDP 443）放行即可。
压缩不用 Caddy 操心：静态文件由服务端发预压缩的 `.br`/`.gz`，API 的 JSON 由服务端自己 gzip。
不要再加 `encode` 指令，那只会在每个请求上重复做一遍白工。

### SSE

发消息和事件流（`POST /api/v1/conversations/{id}/messages`、`GET .../events`）是 SSE。Caddy 的
`reverse_proxy` 默认不缓冲响应，逐帧转发，所以不需要加任何东西。服务端对流式响应还会发
`X-Accel-Buffering: no`，给 nginx 一类默认缓冲的代理看。

## nginx（备选）

```nginx
server {
    listen 443 ssl http2;
    server_name example.com;
    # ssl_certificate / ssl_certificate_key 自行配置

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    # 事件流和发消息：不能缓冲，也不能让读超时把长连接掐掉
    location ~ ^/api/v1/conversations/[^/]+/(events|messages)$ {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_http_version 1.1;
        proxy_set_header Connection "";
        proxy_buffering off;
        proxy_cache off;
        proxy_read_timeout 1h;
    }
}
```

nginx 默认会再对响应做一次 gzip 判断；服务端已经带 `Content-Encoding` 的响应它不会重复压缩。
想要 HTTP/3 需要 nginx 1.25+ 并另外配置 `listen 443 quic`，不如 Caddy 省事。
