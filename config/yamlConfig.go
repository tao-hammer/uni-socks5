package config

type Config struct {
	Port             int64  `yaml:"port"`
	SkipPrivateCheck bool   `yaml:"skipPrivateCheck"`
	CoreNum          int    `yaml:"coreNum"`
	LogFlag          bool   `yaml:"logFlag"`
	LogDir           string `yaml:"logDir"`
	UdpTimeout       int    `yaml:"udpTimeout"`
	User             struct {
		Username string `yaml:"username"`
		Password string `yaml:"password"`
	} `yaml:"user"`
}
