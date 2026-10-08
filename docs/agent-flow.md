# agent の処理フロー

VNI(tap)の増減と、Port から読んだフレームの処理の流れ。全体像は [design.md](design.md) を参照。

## コンポーネント

```mermaid
flowchart LR
    hook["hookscript<br/>(post-start / post-stop)"]
    subgraph agent["vpc-agent (1ホスト1プロセス)"]
        src["Source<br/>ctl.Server"]
        mgr["Sink<br/>agent.Manager"]
        h["FrameHandler<br/>dhcp.Handler"]
        map["mapping.Service"]
        src -->|Attach / Detach| mgr
        src -->|Register / Unregister| map
        mgr -->|VNIForPort| map
        mgr -->|goroutine ごとに HandleFrame| h
        h -->|VNIForPort / Lookup| map
    end
    tap["tap (Port)"]
    hook -->|"PUT / DELETE /vnis/{name}<br/>(unix socket)"| src
    mgr <-->|ReadFrame / WriteFrame| tap
```

`Source` は将来、マッピングサービスの Watch を受ける実装に差し替える。
その場合 `Register` / `Unregister` は不要になり、Sink より下は変わらない。

## VNI の追加 (PUT)

```mermaid
sequenceDiagram
    participant H as hookscript
    participant C as ctl.Server
    participant R as Registry(Mock)
    participant M as Manager
    participant P as Port

    H->>C: PUT /vnis/{name} {vpc_id, mac}
    alt JSON または MAC が不正
        C-->>H: 400
    end
    C->>R: Register(VNI)
    C->>M: Attach(name)
    M->>R: VNIForPort(name)
    alt 未登録
        M-->>C: ErrUnknownVNI
        C->>R: Unregister(name)
        C-->>H: 400
    else Attach 済み
        M-->>C: ErrAlreadyAttached
        Note over C,R: 動作中の Port の登録は消さない
        C-->>H: 409
    else Open に失敗
        M-->>C: error
        C->>R: Unregister(name)
        C-->>H: 500
    else 成功
        M->>P: Open(name)
        M->>M: go serve(port)
        M-->>C: nil
        C-->>H: 204
    end
```

## VNI の削除 (DELETE)

```mermaid
sequenceDiagram
    participant H as hookscript
    participant C as ctl.Server
    participant R as Registry(Mock)
    participant M as Manager
    participant P as Port

    H->>C: DELETE /vnis/{name}
    C->>M: Detach(name)
    alt Attach されていない
        M-->>C: ErrNotAttached
    else
        M->>P: Close()
        P-->>M: 読み取りループが ErrClosed で終了
        M-->>C: nil
    end
    C->>R: Unregister(name)
    C-->>H: 204 (未 Attach なら 404)
```

未 Attach でも `Unregister` は行う。

## フレームの処理 (Port ごとの goroutine)

```mermaid
flowchart TD
    A["ReadFrame"] --> B{"エラー?"}
    B -->|ErrClosed| Z["ループ終了<br/>(Detach / Close による)"]
    B -->|その他| F["ログ出力し、自分が登録した Port なら<br/>Manager から取り除いて Close<br/>他の Port は止めない"]
    F --> Z
    B -->|なし| C["Handler.HandleFrame"]
    C --> D{"handled?"}
    D -->|エラー| E["ログ出力"] --> A
    D -->|true| A
    D -->|false| G["Ethernet ヘッダをログ出力<br/>(転送は今後)"] --> A
```

### DHCP の判定 (`dhcp.Handler.HandleFrame`)

```mermaid
flowchart TD
    A["フレーム"] --> B{"クライアント→サーバの<br/>DHCP か?"}
    B -->|No| N["handled=false"]
    B -->|Yes| C["VNIForPort(port名)"]
    C --> D{"VNI として登録済み?"}
    D -->|No| I["無視 (handled=true)"]
    D -->|Yes| E{"L2 送信元 と CHAddr が<br/>どちらも VNI の MAC と一致?"}
    E -->|No| I
    E -->|Yes| F{"メッセージ種別"}
    F -->|Discover| G["Lookup(VPCID, MAC) → Offer"]
    F -->|Request| H["Lookup(VPCID, MAC) と RequestedIP が一致 → Ack"]
    F -->|その他| I
    G --> W["同じ Port へ WriteFrame"]
    H --> W
```
