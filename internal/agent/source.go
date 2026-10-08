package agent

// Sink は Port の増減を受け付ける。Manager が実装する。
type Sink interface {
	Attach(name string) error
	Detach(name string) error
}

// Source は Port の増減を知り、Sink に伝える。
// 現状は hookscript から叩く ctl.Server。マッピングサービスの Watch で
// この agent のホストに VNI が現れた/消えたことを受ける実装にも、同じ形で差し替えられる。
type Source interface {
	// Run は Close されるか失敗するまで Sink に伝え続ける。
	Run(sink Sink) error
	Close() error
}
