# 開発環境

PVE 実機なしで、かつ macOS に依存しない形で開発する。
Linux コンテナ内の network namespace で PVE と VM を模擬する。

## 方針

- macOS には tap / AF_PACKET が無いため、Linux カーネル上（Docker / OrbStack / Lima など）で動かす。
  中身は素の Linux 機能だけなので、Linux 実機や CI でもそのまま動く。
- OS 非依存のロジック（パーサ、ARP 代理応答、conntrack、FW / NAT、カプセル化）は純 Go のユニットテストで検証する。
  これらは macOS 上でも `go test` で動く。
- 結合テストだけを netns 環境で行う。

## 構成

```
[vm1] eth0 ─veth─ tap-vm1 [pve1] pve1-ul ─veth─ br-ul ─veth─ pve2-ul [pve2] tap-vm2 ─veth─ eth0 [vm2]
```

| 実物 | 模擬 |
|------|------|
| PVE ホスト | netns `pve1`, `pve2` |
| VM | netns `vm1`, `vm2` |
| tap (`tap<vmid>i0`) | veth (`tap-vm1`, `tap-vm2`) |
| 物理スイッチ (AT-x510) | Linux ブリッジ `br-ul` |

アドレス:

- VM: IP は DHCP で取得（`vm1`/`vm2` の MAC は `02:00:00:00:00:0N`）
- underlay: `pve1` = 192.168.100.1, `pve2` = 192.168.100.2
- VM の MTU は 1450（カプセル化分を引いている）

## 使い方

```bash
# macOS 側: ユニットテスト
make test

# Linux コンテナに入る（--privileged）
make lab-shell

# 以下はコンテナ内
make netns-up     # netns を構築
make build
make smoke        # vm1 の ARP が pve1 上の agent に届くか確認
make netns-down
```

手動で試す場合:

```bash
ip netns exec pve1 ./bin/vpc-agent -port-if tap-vm1 -vni-mac 02:00:00:00:00:01 &
ip netns exec vm1 ping 10.10.0.254
ip netns exec pve1 tcpdump -i tap-vm1 -e
```

## Port の差し替え

agent は `-port-kind` と `-port-if` で Port 実装とインターフェース名を受け取る。
tap でも veth でも Go アプリから見れば「L2 フレームを読み書きする NIC」なので、コードは同じ。

## 制約

- Docker Desktop など macOS 上のコンテナでは性能が実機を反映しない。性能検証は Linux 実機かクラウド VM で行う。
- Proxmox の hookscript による tap 切り離しはこの環境で再現できない。最終的に実機で少量だけ確認する。
- XDP / eBPF を使う場合はコンテナ側カーネルの対応状況を要確認。
