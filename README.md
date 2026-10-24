# clash-exporter

Prometheus exporter for **OpenClash / Mihomo**（也兼容旧版 Clash 的 external controller）。

Fork 自 [zxh326/clash-exporter](https://github.com/zxh326/clash-exporter)，按当前 Mihomo（OpenClash 正在用的内核）改过：

- `/version` 认 `meta: true`。原仪表盘的 `premium` 标签在 Mihomo 上也会是 `true`，Grafana 面板 [18530](https://grafana.com/grafana/dashboards/18530-clash-dashboard/) 不用改。
- 连接流量用 `int64`。原来的 `int` 在 32 位 OpenWrt 上会溢出。
- `chains` 为空不再崩。策略标签取 Mihomo 的实际出口（`chains[0]`）。
- 目的地优先用嗅探域名 `sniffHost`。
- 计数器只加非负增量，核心重启把连接计数清掉时不会把 Counter 减成负数。
- WebSocket 用 `Authorization: Bearer`，密钥里有特殊字符也能过。
- 新增 `clash_memory_bytes`、`clash_mode`。
- 当前 Mihomo 已经去掉 `/profile/tracing`。`-collectTracing` 打开后如果内核返回 404，这个采集器会自己停掉，不再把进程打死。
- 路由器上默认**不**按目的地拆指标（`-collectDest` 默认 `false`）。

## OpenWrt / OpenClash

仓库里的 [openwrt/Makefile](openwrt/Makefile) 是给 ImmortalWrt / OpenWrt 编译用的包。装上之后：

- 监听 `0.0.0.0:2112`，防火墙默认挡住 WAN。抓取地址：`http://路由器:2112/metrics`
- `openclash_autoconf=1`（默认）时，从 UCI `openclash.config.cn_port` 和 `openclash.config.dashboard_password` 读控制器。密码为空就再看运行中的配置文件里的 `secret:`。
- 连接地址固定是 `127.0.0.1`，因为 OpenClash 的 `external-controller` 经常写成 `0.0.0.0:9090`，不能拿来当客户端地址。

```
/etc/init.d/clash-exporter enable
/etc/init.d/clash-exporter restart
```

要按目的地统计：

```
uci set clash-exporter.main.collect_dest='1'
uci commit clash-exporter
/etc/init.d/clash-exporter restart
```

OpenClash 改了面板端口或密码之后，这个服务会跟着 reload（`procd` 监听了 `openclash` 的配置）。

## 命令行

```
./clash-exporter -h
./clash-exporter -listen 0.0.0.0:2112 -collectDest=false
```

环境变量：`CLASH_HOST`（默认 `127.0.0.1:9090`）、`CLASH_TOKEN`。

## 指标

| 名字 | 类型 | 标签 |
| --- | --- | --- |
| clash_info | Gauge | version, premium |
| clash_download_bytes_total | Gauge | |
| clash_upload_bytes_total | Gauge | |
| clash_active_connections | Gauge | |
| clash_network_traffic_bytes_total | Counter | source, destination, policy, type |
| clash_memory_bytes | Gauge | type=inuse, oslimit |
| clash_mode | Gauge | mode |
| clash_tracing_* | Histogram | 仅当内核还提供 /profile/tracing |

Prometheus：

```yaml
- job_name: clash
  metrics_path: /metrics
  scrape_interval: 15s
  static_configs:
    - targets: ["192.168.1.1:2112"]
```

`scrape_interval` 不要用原来的 1 秒。流量来自 `/connections` 的推送，1 秒抓一次只会放大 Prometheus 的压力。
