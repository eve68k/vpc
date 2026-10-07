# DHCP実装プラン（仮）

参照: [issue #3](https://github.com/eve68k/vpc/issues/3)

このメモは一時的なプランニング資料ですわ。実装に着手する際は内容を見直してくださいませ。

## issueの論点への回答

### ① フレームの識別手法

一発で判定する関数は作らず、**段階的な浅い判定 → 確定したら構造体へ本格パース**の二段構成にする。

受け取ったバッファに対してオフセット直読みで順にチェックする。

1. EtherType（offset 12-13）が `0x0800`（IPv4）か
2. IPv4ヘッダのProtocol（offset 9）が `17`（UDP）か
3. UDPヘッダのポートが src=68, dst=67（クライアント→サーバ方向）か

すべて通ったらDHCPメッセージ構造体へパースする。無関係なフレーム（将来のARPや通常のIP通信）を毎回フルパースせずに弾けるため、`port.Port.ReadFrame` のホットパスでも安価。

### ② switch か if か

- 分岐先が3つ以上に増える可能性がある列挙的な値（EtherType、IPのProtocol、DHCPのメッセージタイプ=option 53）→ `switch`
- 単純な2値のゲート条件（UDPポートが67/68かどうかの一致チェックなど）→ `if` で早期return

DHCPのメッセージタイプ（DISCOVER/OFFER/REQUEST/ACK/…）は将来RELEASEやNAK等が増える前提の列挙なので `switch` にする。

## パッケージ構成

`internal/dhcp/` をサブパッケージとして切る。

- `internal/dhcp/dhcp.go` — DHCPメッセージのParse/Build
- `internal/dhcp/handler.go` — Discover/Offer, Request/Ackのハンドリング

## フェーズ分け

### Phase 1 — パケット解析の土台

Ethernet/IPv4/UDPの最小限のオフセット読み取りヘルパー。ゼロコピーで読むだけの薄い層にして、将来のARP代理応答にも使い回せる形にする。

### Phase 2 — DHCPメッセージのモデル

RFC 2131準拠のフィールド（op, xid, flags, ciaddr, yiaddr, chaddr, magic cookie, オプションTLVなど）を持つ構造体と、`Parse([]byte) (*Message, error)` / `(*Message) Build() []byte` を用意する。

オプションは最小限から始める。

- message type (53)
- requested IP (50)
- server identifier (54)
- lease time (51)
- subnet mask (1)

router (3) や DNS (6) は後回し。

### Phase 3 — MappingServiceのモック

`MAC → IP` を問い合わせる単一のインターフェースを切り、インメモリの簡易実装（プールから順に割り当てるだけ等）を用意する。

- 本物のMapping Serviceとの通信は後続issueに回す
- キャッシュを内部に持つかどうかはこの実装側の詳細であり、`dhcp` パッケージ側は関知しない

### Phase 4 — Discover→Offer / Request→Ackのハンドラ

- Discoverハンドラ：MappingServiceに問い合わせて空きIPを割り当ててもらい、Offerを返す
- Requestハンドラ：**同じMappingServiceに再度問い合わせて**、そのMAC↔IPの対応が存在するか確認してからAckを返す（存在しなければ無視、あるいはNAK）

エージェント自身はフロー全体の状態遷移を追わない（Discoverを覚えていなくてもRequestだけ単独で処理できる）というステートレス方針を維持する。MAC↔IPの対応という「状態」はMappingService側が正本として持つ。

### Phase 5 — レスポンス送出

Offer/AckフレームをEthernet+IPv4+UDP+DHCPで組み立てて `port.Port.WriteFrame` に渡す。エージェントが新規に組み立てて送るOffer/Ack側は正しいIP/UDPチェックサムを計算する。

### Phase 6 — テスト

`port/mem_test.go` の作法（振る舞い単位の日本語テスト名）を踏襲する。

- DHCPパッケージ単体：Discover/Offer/Request/Ackそれぞれのbuild→parseのラウンドトリップ、オプション欠落時の挙動などをテーブル駆動で
- 識別ロジック：EtherType違い/Protocol違い/ポート違いでそれぞれ「DHCPではないと判定される」ケース
- `port.NewMemPair` を使った結合寄りのテスト：片方にDiscoverフレームを書き込み、ハンドラを通した結果が反対側にOfferとして届くか

### Phase 7 — `vpc-agent` への組み込み

現在ログだけのループに識別→分岐を差し込み、DHCP以外のフレームは当面無視（または既存ログ）にする。

## Phase 8（仮）— MappingService連携への移行プラン

現状、`dhcp-server-ip` / `dhcp-subnet-mask` / `dhcp-lease-seconds` / `dhcp-pool` は起動フラグで固定しているが、
本来これらはVPCごとにMappingServiceから得る情報。以下の論点を決定した。

### 決定事項

1. **問い合わせ方式**：VPC単位の静的情報（サブネット、リース時間）とMACごとの割当IPを
   1回の `Lookup` にバンドルして返す。`dhcp.Handler` は都度問い合わせるだけで、
   キャッシュするかどうかは `mapping.Service` の実装（モック or 本物のクライアント）側の詳細とする
   （既存のPhase3方針「キャッシュを内部に持つかどうかはこの実装側の詳細」を踏襲）。
2. **Port→VPCの解決**：`port.Port.Name()` をキーに、MappingServiceへ別途問い合わせる
   （`VPCForPort`）。`dhcp.Handler` は自前の状態を持たず、`HandleFrame` の中で毎回解決する
   （Discoverを覚えていなくてもRequestだけ処理できる、という既存のステートレス方針と一致）。
3. **DHCPサーバを名乗るMAC/IP**：MACはVPC共通の固定値（既存の `-dhcp-server-mac` 相当）を維持する。
   IP（ゲートウェイ・サーバ識別子）はVPCごとのサブネットの「ネットワークアドレス + 1」として
   都度計算する。MappingServiceはゲートウェイIPを直接返す必要はなく、サブネット
   （ネットワークアドレス + マスク）さえ返せばdhcp側で導出できる。
4. **キャッシュ**：`dhcp.Handler` / `vpc-agent` はキャッシュを持たない。キャッシュするかどうかは
   `mapping.Service` の実装側の責務とする（本物のMappingServiceクライアントの実装issueに回す）。

### インターフェース変更案

```go
package mapping

// VPCID は VPC の識別子。独自のカプセル化ヘッダにそのまま埋め込むため、
// 可変長の string ではなく固定長の整数にする（design.md の VNI 相当）。
type VPCID uint32

// Lease は VPC 内で mac に割り当てる（または割り当て済みの）リース情報。
type Lease struct {
	IP        net.IP
	Subnet    *net.IPNet // ネットワークアドレス + マスク。ゲートウェイはSubnetから導出する。
	LeaseTime uint32
}

type Service interface {
	// VPCForPort は port 名からその port が所属する VPC の識別子を返す。
	VPCForPort(portName string) (vpcID VPCID, ok bool)
	// Lookup は vpcID 内で mac に割り当てるリース情報を返す。
	Lookup(vpcID VPCID, mac net.HardwareAddr) (lease Lease, ok bool)
}
```

### `dhcp.Handler` の変更

```go
type Handler struct {
	Mapping   mapping.Service
	ServerMAC net.HardwareAddr // VPC共通の固定MAC
}
```

- `HandleFrame` 内で `p.Name()` → `VPCForPort` → `vpcID` を解決し、`Lookup(vpcID, mac)` でリースを得る。
- `ServerIP`（ゲートウェイ兼サーバ識別子）は `lease.Subnet` のネットワークアドレス+1として
  都度計算するヘルパー（例: `gatewayOf(subnet *net.IPNet) net.IP`）を新設する。
- `SubnetMask` / `LeaseTime` は `lease` から取る（Handlerのフィールドからは外す）。

### `mock.go` の変更

- 単一プールではなく、`VPCID(uint32) → (サブネット, プール, リース時間)` のマップと、
  `Port名 → VPCID` のマップを持つ。
- サブネットはVPC固有の情報であり、`Lease.Subnet` として `mapping.Service` が返す。
  モックはそれを内部に持つ値として保持し、`dhcp` / `main.go` 側は関知しない。
- 現状1エージェント=1Portなので、モックの設定もその範囲で十分
  （複数VPC対応は本物のMappingService連携issueに回す）。

### `main.go` / フラグの変更

- 削除：`-dhcp-server-ip`、`-dhcp-subnet-mask`、`-dhcp-lease-seconds`、`-dhcp-pool`
  （サブネット・リース時間・プールはいずれもVPC固有の情報なので、フラグでは受けず
  MappingServiceから取得する。`-dhcp-subnet` は追加しない）
- 追加：`-dhcp-vpc-id`（モックが「このPortはこのVPC」と答えるための値。`uint32` としてパースする）
- 維持：`-dhcp-server-mac`（VPC共通の固定値のため）
- モックが返すサブネット・リース時間・プールは、`NewMock` の引数としてモック生成箇所で与える。
  本物のクライアントに差し替える時点でこの配線ごと不要になる。

### 残課題（後続issue）

- 本物のMappingServiceとの通信（HTTP/gRPCなど）実装
- Port→VPCの解決結果や静的VPC情報を、本物のクライアント側でどうキャッシュ・Watchするか
- 1エージェントが複数Portを扱うようになった場合の `VPCForPort` 呼び出し頻度の最適化
