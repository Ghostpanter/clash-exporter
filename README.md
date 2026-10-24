# clash-exporter

Prometheus exporter for **OpenClash / Mihomo**（也兼容旧版 Clash 的 external controller）。

Fork 自 [zxh326/clash-exporter](https://github.com/zxh326/clash-exporter)，按当前 Mihomo（OpenClash 正在用的内核）改过：

- `/version` 认 `meta: true`。原仪表盘的 `premium` 标签在 Mihomo 上也会是 `true`，Grafana 面板 [18530](https://grafana.com/grafana/dashboards/18530-clash-dashboard/) 不用改。
- 连接流量用 `int64`。原来的 `int` 在 32 位 OpenWrt 上会溢出。
- `chains` 为空不再崩。策略标签取 Mihomo 的实际出口（`chains[0]`）。
- 目的地优先用嗅探域名 `sniffHost`。
- 计数器只加非负增量。同一条连接的字节变小（内核把这条连接的计数清掉）时不会把 Counter 减成负数。
- 每次连上 `/connections` 的第一帧只做基线：当时已经在跑、而且之前没见过的连接，上面的历史字节不进 `clash_network_traffic_bytes_total`。否则 exporter 或内核一重启，Grafana 就会出现几 GB 的尖刺。之后新出现的连接从第一次看见开始计，只活了一帧的短连接也能算上；已经在跟踪的连接只加增量，websocket 断开期间涨上去的也算。看总流量用下面两个 Gauge。
- WebSocket 用 `Authorization: Bearer`，密钥里有特殊字符也能过。
- 新增 `clash_memory_bytes`、`clash_mode`。Mihomo 会把 `/memory` 的第一帧强写成 0，好让它自己的图从原点画起；这个假 0 会丢掉，避免重连时内存曲线掉到 0。第一帧如果本来就不是 0，会照常记。
- 当前 Mihomo 已经去掉 `/profile/tracing`。`-collectTracing` 打开后如果内核返回 404，这个采集器会自己停掉，不再把进程打死。
- 路由器上默认**不**按目的地拆指标（`-collectDest` 默认 `false`）。
- 新增 `clash_up`。内核停掉（比如 OpenClash 重启、更新规则）时 `clash_up` 变 0，`clash_mode`、`clash_info`、`clash_memory_bytes` 清空，`clash_active_connections` 归 0，不再一直报最后一次的旧值。
- 采集器断开重连的等待时间在正常跑满 1 分钟后重新从 10 秒算起，不会在 OpenClash 重启几次之后一直卡在 60 秒。

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
| clash_up | Gauge | |
| clash_info | Gauge | version, premium |
| clash_download_bytes_total | Gauge | |
| clash_upload_bytes_total | Gauge | |
| clash_active_connections | Gauge | |
| clash_network_traffic_bytes_total | Counter | source, destination, policy, type |
| clash_memory_bytes | Gauge | type=inuse, oslimit |
| clash_mode | Gauge | mode |
| clash_tracing_* | Histogram | 仅当内核还提供 /profile/tracing |

`clash_network_traffic_bytes_total` 是按 Mihomo 大约每秒一次的连接快照算增量的。连上之后的第一帧是基线，不计入历史流量。一条连接在两次快照之间结束，它最后不到 1 秒的流量就算不进去。要看总流量用 `clash_download_bytes_total` / `clash_upload_bytes_total`（内核自己的累计值，连上的当下就会写成控制器返回的绝对值，内核重启后从 0 开始）。按来源 / 策略拆分的 Counter 只当比例参考。

Prometheus：

```yaml
- job_name: clash
  metrics_path: /metrics
  scrape_interval: 15s
  static_configs:
    - targets: ["192.168.1.1:2112"]
```

`scrape_interval` 不要用原来的 1 秒。流量来自 `/connections` 的推送，1 秒抓一次只会放大 Prometheus 的压力。
