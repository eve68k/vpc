# 設計メモ

Proxmox VE 上で、分散 NAT / 分散 FW / 分散ルーターを実現するための設計方針。
構成図は [architecture.drawio.svg](architecture.drawio.svg) を参照。

## 基本方針

「パケットを覗く」のではなく **VNI の L2 の出口を Go アプリが握る**。
各 PVE ホストの Go アプリ (`vpc-agent`) が、その PVE 上の VNI が出すフレームをすべて受け取り、
宛先 VNI の存在する PVE ホストへカプセル化して転送する。

ブロードキャスト / マルチキャストはサポートしない（ARP は agent が代理応答する）。

異なる VPC 間は常に隔離する（ピアリングやルーティングは対象外）。
宛先の検索キーは `(VPCID, IP)` とし、送信元の VNI から VPCID を決める。
別 VPC の VNI は検索で引けないため、ARP 代理応答も転送も行われない。
VPC が異なれば IP や MAC が重なってよい。

## データフロー

構成図の番号に対応する。

| # | 経路 | 内容 |
|---|------|------|
| ① | VNI → Go アプリ | VNI の Port (`vni.Port`) から L2 フレームを読む |
| ② | Go アプリ → マッピングサービス | 宛先 `(VPCID, IP)` の所在を問い合わせる |
| ③ | マッピングサービス → Go アプリ | 宛先 VNI が属する PVE ホストの IP と宛先 MAC を返す |
| ④ | Go アプリ → Go アプリ | フレームをカプセル化して宛先 PVE へ UDP 送信 |
| ⑤ | 宛先 Go アプリ → マッピングサービス | 送信元の検証（送信元 VNI の `(VPCID, MAC, IP)` が正当か問い合わせる） |
| ⑥ | マッピングサービス → 宛先 Go アプリ | ⑤ の検証結果を返す |
| ⑦ | Go アプリ → VNI | デカプセル化して宛先 VNI の Port へ書き込む |

## 各コンポーネント

### Port（VNI の L2 出口）

`vni.Port` インターフェースで抽象化する。VNI 1 枚の L2 の読み書き口であり、VNI と 1 対 1 に対応する。

- 本番: VM の NIC ごとにできる PVE の tap を `vmbr` から切り離し (`ip link set tapXXXi0 nomaster`)、
  agent が直接読み書きする。この tap が VNI の Port になる。
  切り離しは Proxmox の hookscript (post-start) で NIC ごとに自動化する。VM の NIC は `firewall=0` にする。
- 開発: veth（[dev-environment.md](dev-environment.md)）。
- テスト: メモリ実装 (`vni.NewMemPair`)。
- 実装は AF_PACKET を基本とする。XDP / AF_XDP は README の通り現時点では対象外で、
  `vni.Port` の差し替えで後から導入できる構造にしておく。

### マッピングサービス

`(VPCID, 宛先 IP) → 宛先 VNI が属する PVE ホスト IP + 宛先 MAC` を返す。
また、Port 名から VNI（VPCID、MAC、IP、サブネット）を引ける。
agent は結果をキャッシュし、変更は Watch / Push で反映する。

### カプセル化

独自ヘッダを UDP に載せる。VPC 識別のため `VPCID` をヘッダに含める。

## 機能

- **分散ルーター**: VM のデフォルト GW を仮想 IP + 固定の仮想 MAC にする。ARP は agent が代理応答する。
- **分散 FW**: 5 タプルの conntrack を agent 内に持ち、ルールを評価する。
- **分散 NAT**: conntrack に NAT エントリを持たせ、IP / ポートを書き換える。IP / L4 チェックサムの再計算が必要。
  外部への出口は特定ホストまたは GW に集約する。

### 1 ホスト内の転送

宛先 VNI が同じ agent 配下にある場合は、カプセル化せず宛先の Port へ直接 `WriteFrame` する。
VPC が同じであることは、検索キーの `VPCID` で保証される。

## 注意点

1. **オフロード**: virtio は checksum / TSO / GSO を省略したフレームを出す。
   tap で `ethtool -K <if> tx off tso off gso off` を設定するか、部分チェックサムを自前で処理する。
2. **MTU**: カプセル化で約 50 バイト増える。物理側 (AT-x510) をジャンボフレーム化するか、VM 側 MTU を下げる。
3. **VM マイグレーション**: VNI の所在が変わるため、tap の付け替えとマッピングの更新が必要。
4. **性能**: AF_PACKET は数 Gbps 付近が限界になりやすい。マルチキュー、CPU 固定などで対処する。

## 実装ステップ

1. hookscript で tap を切り離す。
2. tap の読み書きループと ARP 代理応答を作る。
3. マッピングサービスをモックし、UDP カプセル化で VM 間通信を通す。
4. conntrack、FW、NAT を順に足す。
