package config

type Config struct {
	Port             int64  `yaml:"port"`
	SkipPrivateCheck bool   `yaml:"skipPrivateCheck"`
	CoreNum          int    `yaml:"coreNum"`
	LogFlag          bool   `yaml:"logFlag"`
	UdpTimeout       int    `yaml:"udpTimeout"`
	MaxConns         int    `yaml:"maxConns"`
	AuthLimit        int    `yaml:"authLimit"`
	AuthBlock        int    `yaml:"authBlock"`
	IdleTimeout      int    `yaml:"idleTimeout"`
	User             struct {
		Username string `yaml:"username"`
		Password string `yaml:"password"`
	}
}
