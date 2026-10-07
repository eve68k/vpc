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
