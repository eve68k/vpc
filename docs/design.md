# 設計メモ

Proxmox VE 上で、分散 NAT / 分散 FW / 分散ルーターを実現するための設計方針。
構成図は [architecture.drawio.svg](architecture.drawio.svg) を参照。

## 基本方針

「パケットを覗く」のではなく **VM の L2 の出口を Go アプリが握る**。
各 PVE ホストの Go アプリ (`vpc-agent`) が、その PVE 上の VM が出すフレームをすべて受け取り、
宛先 VM の存在する PVE ホストへカプセル化して転送する。

ブロードキャスト / マルチキャストはサポートしない（ARP は agent が代理応答する）。

## データフロー

構成図の番号に対応する。

| # | 経路 | 内容 |
|---|------|------|
| ① | VM → Go アプリ | VM の tap (`tap<vmid>i0`) から L2 フレームを読む |
| ② | Go アプリ → マッピングサービス | 宛先 (IP / テナント) の所在を問い合わせる |
| ③ | マッピングサービス → Go アプリ | 宛先 PVE ホストの IP と宛先 MAC を返す |
| ④ | Go アプリ → Go アプリ | フレームをカプセル化して宛先 PVE へ UDP 送信 |
| ⑤ | 宛先 Go アプリ → マッピングサービス | （未確定）送信元の検証、または VM 位置の登録 |
| ⑥ | マッピングサービス → 宛先 Go アプリ | （未確定）⑤ の応答 |
| ⑦ | Go アプリ → VM | デカプセル化して宛先 VM の tap へ書き込む |

⑤⑥ の役割は要確認。

## 各コンポーネント

### Port（VM の L2 出口）

`port.Port` インターフェースで抽象化する。

- 本番: PVE の tap を `vmbr` から切り離し (`ip link set tapXXXi0 nomaster`)、agent が直接読み書きする。
  切り離しは Proxmox の hookscript (post-start) で自動化する。VM の NIC は `firewall=0` にする。
- 開発: veth（[dev-environment.md](dev-environment.md)）。
- テスト: メモリ実装 (`port.NewMemPair`)。
- 実装は AF_PACKET を基本とする。XDP / AF_XDP は README の通り現時点では対象外で、
  `Port` の差し替えで後から導入できる構造にしておく。

### マッピングサービス

`宛先 IP (+ テナント) → 宛先 PVE ホスト IP + 宛先 MAC` を返す。
agent は結果をキャッシュし、変更は Watch / Push で反映する。

### カプセル化

VXLAN / Geneve、または独自ヘッダを UDP に載せる。テナント識別のため VNI 相当を付ける。

## 機能

- **分散ルーター**: VM のデフォルト GW を仮想 IP + 固定の仮想 MAC にする。ARP は agent が代理応答する。
- **分散 FW**: 5 タプルの conntrack を agent 内に持ち、ルールを評価する。
- **分散 NAT**: conntrack に NAT エントリを持たせ、IP / ポートを書き換える。IP / L4 チェックサムの再計算が必要。
  外部への出口は特定ホストまたは GW に集約する。

## 注意点

1. **オフロード**: virtio は checksum / TSO / GSO を省略したフレームを出す。
   tap で `ethtool -K <if> tx off tso off gso off` を設定するか、部分チェックサムを自前で処理する。
2. **MTU**: カプセル化で約 50 バイト増える。物理側 (AT-x510) をジャンボフレーム化するか、VM 側 MTU を下げる。
3. **VM マイグレーション**: tap の付け替えとマッピングの更新が必要。
4. **性能**: AF_PACKET は数 Gbps 付近が限界になりやすい。マルチキュー、CPU 固定などで対処する。

## 実装ステップ

1. hookscript で tap を切り離す。
2. tap の読み書きループと ARP 代理応答を作る。
3. マッピングサービスをモックし、UDP カプセル化で VM 間通信を通す。
4. conntrack、FW、NAT を順に足す。
